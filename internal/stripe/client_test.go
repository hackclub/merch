package stripe

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateCheckout(t *testing.T) {
	var form map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/checkout/sessions" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer sk_test_x" {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Idempotency-Key") != "merch-request-7-pay-1" {
			t.Errorf("idempotency key = %q", r.Header.Get("Idempotency-Key"))
		}
		r.ParseForm()
		form = map[string]string{}
		for k := range r.PostForm {
			form[k] = r.PostForm.Get(k)
		}
		json.NewEncoder(w).Encode(map[string]any{"id": "cs_test_1", "url": "https://checkout.stripe.com/c/pay/cs_test_1", "status": "open", "payment_status": "unpaid", "amount_total": 1500, "currency": "usd", "payment_intent": nil})
	}))
	defer srv.Close()

	s, err := New(srv.URL, "sk_test_x", nil).CreateCheckout(context.Background(), CheckoutInput{
		RequestID: 7, AmountCents: 1500, Name: "Shipping for swag request #7", Email: "orpheus@hackclub.com",
		SuccessURL: "http://app.test/requests?paid=7", CancelURL: "http://app.test/requests", IdempotencyKey: "merch-request-7-pay-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != "cs_test_1" || s.URL == "" || s.Status != "open" || s.AmountTotal != 1500 {
		t.Fatalf("session = %+v", s)
	}
	for k, want := range map[string]string{
		"mode":                                          "payment",
		"line_items[0][quantity]":                       "1",
		"line_items[0][price_data][currency]":           "usd",
		"line_items[0][price_data][unit_amount]":        "1500",
		"line_items[0][price_data][product_data][name]": "Shipping for swag request #7",
		"client_reference_id":                           "7",
		"metadata[request_id]":                          "7",
		"payment_intent_data[metadata][request_id]":     "7",
		"customer_email":                                "orpheus@hackclub.com",
		"success_url":                                   "http://app.test/requests?paid=7",
		"cancel_url":                                    "http://app.test/requests",
	} {
		if form[k] != want {
			t.Errorf("%s = %q, want %q", k, form[k], want)
		}
	}
}

func TestGetCheckoutAndErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/checkout/sessions/cs_paid":
			json.NewEncoder(w).Encode(map[string]any{"id": "cs_paid", "status": "complete", "payment_status": "paid", "amount_total": 900, "currency": "usd",
				"payment_intent": "pi_1", "client_reference_id": "3", "metadata": map[string]string{"request_id": "3"}})
		case "/v1/checkout/sessions/cs_open/expire":
			if r.Method != "POST" {
				t.Errorf("expire method %s", r.Method)
			}
			json.NewEncoder(w).Encode(map[string]any{"id": "cs_open", "status": "expired"})
		default:
			w.WriteHeader(404)
			w.Write([]byte(`{"error":{"message":"No such checkout.session: 'cs_nope'"}}`))
		}
	}))
	defer srv.Close()
	c := New(srv.URL, "sk_test_x", nil)

	s, err := c.GetCheckout(context.Background(), "cs_paid")
	if err != nil || !s.Paid() || s.PaymentIntent != "pi_1" || s.RequestID() != 3 || s.AmountTotal != 900 {
		t.Fatalf("get = %+v, %v", s, err)
	}
	if _, err := c.GetCheckout(context.Background(), "cs_nope"); err == nil || err.Error() != "stripe: 404: No such checkout.session: 'cs_nope'" {
		t.Fatalf("err = %v", err)
	}
	if err := c.ExpireCheckout(context.Background(), "cs_open"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetCheckout(context.Background(), "../x"); err == nil {
		t.Fatal("bad id must be refused before the call")
	}
}
