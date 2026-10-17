package stripe

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreatePayment(t *testing.T) {
	var form map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/payment_intents" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer sk_test_x" {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Idempotency-Key") != "merch-request-7-pay" {
			t.Errorf("idempotency key = %q", r.Header.Get("Idempotency-Key"))
		}
		r.ParseForm()
		form = map[string]string{}
		for k := range r.PostForm {
			form[k] = r.PostForm.Get(k)
		}
		json.NewEncoder(w).Encode(map[string]any{"id": "pi_1", "client_secret": "pi_1_secret_x", "status": "requires_payment_method", "amount": 1500, "currency": "usd"})
	}))
	defer srv.Close()

	p, err := New(srv.URL, "sk_test_x", nil).CreatePayment(context.Background(), PaymentInput{
		RequestID: 7, AmountCents: 1500, Description: "Shipping for Hack Club swag request #7", Email: "orpheus@hackclub.com", IdempotencyKey: "merch-request-7-pay",
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "pi_1" || p.ClientSecret != "pi_1_secret_x" || p.Status != "requires_payment_method" || p.Amount != 1500 {
		t.Fatalf("payment = %+v", p)
	}
	for k, want := range map[string]string{
		"amount":                          "1500",
		"currency":                        "usd",
		"allowed_payment_method_types[0]": "card",
		"description":                     "Shipping for Hack Club swag request #7",
		"receipt_email":                   "orpheus@hackclub.com",
		"metadata[request_id]":            "7",
	} {
		if form[k] != want {
			t.Errorf("%s = %q, want %q", k, form[k], want)
		}
	}
}

func TestGetCancelAndErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/payment_intents/pi_paid":
			json.NewEncoder(w).Encode(map[string]any{"id": "pi_paid", "status": "succeeded", "amount": 900, "amount_received": 900, "currency": "usd",
				"metadata": map[string]string{"request_id": "3"}})
		case "/v1/payment_intents/pi_open/cancel":
			if r.Method != "POST" {
				t.Errorf("cancel method %s", r.Method)
			}
			json.NewEncoder(w).Encode(map[string]any{"id": "pi_open", "status": "canceled"})
		default:
			w.WriteHeader(404)
			w.Write([]byte(`{"error":{"message":"No such payment_intent: 'pi_nope'"}}`))
		}
	}))
	defer srv.Close()
	c := New(srv.URL, "sk_test_x", nil)

	p, err := c.GetPayment(context.Background(), "pi_paid")
	if err != nil || !p.Paid() || p.RequestID() != 3 || p.AmountReceived != 900 {
		t.Fatalf("get = %+v, %v", p, err)
	}
	if _, err := c.GetPayment(context.Background(), "pi_nope"); err == nil || err.Error() != "stripe: 404: No such payment_intent: 'pi_nope'" {
		t.Fatalf("err = %v", err)
	}
	if err := c.CancelPayment(context.Background(), "pi_open"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetPayment(context.Background(), "../x"); err == nil {
		t.Fatal("bad id must be refused before the call")
	}
}

func TestRefund(t *testing.T) {
	var form, key, amount string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/refunds" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		r.ParseForm()
		form, key, amount = r.PostForm.Get("payment_intent"), r.Header.Get("Idempotency-Key"), r.PostForm.Get("amount")
		json.NewEncoder(w).Encode(map[string]any{"id": "re_1", "status": "succeeded"})
	}))
	defer srv.Close()
	c := New(srv.URL, "sk_test_x", nil)
	if err := c.Refund(context.Background(), "pi_paid", 0, "merch-request-7-refund"); err != nil {
		t.Fatal(err)
	}
	if form != "pi_paid" || key != "merch-request-7-refund" || amount != "" {
		t.Fatalf("payment_intent = %q, key = %q, amount = %q", form, key, amount)
	}
	// Part of it: just that amount.
	if err := c.Refund(context.Background(), "pi_paid", 300, "k2"); err != nil || amount != "300" {
		t.Fatalf("partial refund: amount = %q, err = %v", amount, err)
	}
	if err := c.Refund(context.Background(), "bogus", 0, ""); err == nil {
		t.Fatal("bad id refunded")
	}
}

// Stripe can hold a refund for a person to approve in the dashboard; it then
// runs by itself, so callers need to tell that apart from a refusal.
func TestRefundNeedsApproval(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		w.Write([]byte(`{"error":{"message":"This action requires human approval before it can be completed. An approval request has been created and is awaiting review. If approved, it will automatically execute. To check status: GET /v2/core/approval_requests/apreq_61VaTest. To provide context for the reviewer: POST /v2/core/approval_requests/apreq_61VaTest with a reason parameter."}}`))
	}))
	defer srv.Close()
	err := New(srv.URL, "sk_test_x", nil).Refund(context.Background(), "pi_paid", 0, "k")
	var ae *ApprovalError
	if !errors.As(err, &ae) || ae.ID != "apreq_61VaTest" {
		t.Fatalf("err = %#v", err)
	}
}

func TestRefunded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/v1/refunds" || r.URL.Query().Get("payment_intent") != "pi_paid" {
			t.Errorf("%s %s", r.Method, r.URL)
		}
		json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{
			{"id": "re_1", "amount": 300, "status": "succeeded"},
			{"id": "re_2", "amount": 200, "status": "pending"},
			{"id": "re_3", "amount": 900, "status": "failed"},
			{"id": "re_4", "amount": 900, "status": "canceled"},
		}})
	}))
	defer srv.Close()
	cents, err := New(srv.URL, "sk_test_x", nil).Refunded(context.Background(), "pi_paid")
	if err != nil || cents != 500 {
		t.Fatalf("refunded = %d, %v", cents, err)
	}
}
