package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"testing"

	"github.com/hackclub/merch/internal/store"
	"github.com/hackclub/merch/internal/stripe"
	"github.com/hackclub/merch/internal/swag"
)

// fakeStripe stands in for Stripe Checkout.
type fakeStripe struct {
	mu       sync.Mutex
	sessions map[string]*stripe.Session
	created  []stripe.CheckoutInput
	expired  []string
}

func (f *fakeStripe) CreateCheckout(_ context.Context, in stripe.CheckoutInput) (*stripe.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, in)
	id := fmt.Sprintf("cs_test_%d", len(f.created))
	s := &stripe.Session{ID: id, URL: "https://checkout.example/" + id, Status: "open", PaymentStatus: "unpaid", AmountTotal: in.AmountCents, Currency: "usd",
		Metadata: map[string]string{"request_id": strconv.FormatInt(in.RequestID, 10)}}
	if f.sessions == nil {
		f.sessions = map[string]*stripe.Session{}
	}
	f.sessions[id] = s
	cp := *s
	return &cp, nil
}

func (f *fakeStripe) GetCheckout(_ context.Context, id string) (*stripe.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sessions[id]
	if !ok {
		return nil, fmt.Errorf("stripe: 404: no such session")
	}
	cp := *s
	return &cp, nil
}

func (f *fakeStripe) ExpireCheckout(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.expired = append(f.expired, id)
	f.sessions[id].Status = "expired"
	return nil
}

// pay completes a session as if the person paid amount cents on it.
func (f *fakeStripe) pay(id string, amount int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.sessions[id]
	s.Status, s.PaymentStatus, s.AmountTotal, s.PaymentIntent = "complete", "paid", amount, "pi_"+id
}

func newStripeHarness(t *testing.T) (*harness, *fakeStripe) {
	fs := &fakeStripe{}
	return newHarness(t, func(d *Deps) { d.Payments = fs }), fs
}

type viewed struct {
	store.Request
	PaymentURL string `json:"payment_url"`
}

func (h *harness) intlRequest(email string) viewed {
	h.t.Helper()
	it := h.mkItem(fmt.Sprintf("Sti/%s", email), true)
	var r viewed
	if code := h.do(h.client(email), "POST", "/api/requests", map[string]any{"purpose": "personal", "address": deAddr, "lines": []swag.Line{{ItemID: it.ID, Quantity: 1}}}, &r); code != 201 {
		h.t.Fatalf("create = %d", code)
	}
	return r
}

// pay follows the request's payment link and returns where it redirects.
func (h *harness) payLink(c *http.Client, r viewed) (int, string) {
	h.t.Helper()
	resp, err := c.Get(h.srv.URL + r.PaymentURL)
	if err != nil {
		h.t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode, resp.Header.Get("Location")
}

func TestCardPaymentForInternationalShipping(t *testing.T) {
	h, fs := newStripeHarness(t)
	user := h.client(userEmail)
	r := h.intlRequest(userEmail)
	if r.Status != swag.StatusAwaitingPayment || r.PaymentURL != fmt.Sprintf("/api/requests/%d/pay", r.ID) {
		t.Fatalf("created = %+v", r)
	}

	// Only the requester can pay.
	if code, _ := h.payLink(h.client(authorEmail), r); code != 404 {
		t.Fatalf("someone else's pay link = %d", code)
	}

	code, loc := h.payLink(user, r)
	if code != http.StatusSeeOther || loc != "https://checkout.example/cs_test_1" {
		t.Fatalf("pay = %d %q", code, loc)
	}
	in := fs.created[0]
	if in.RequestID != r.ID || in.AmountCents != 1500 || in.Email != userEmail ||
		in.SuccessURL != fmt.Sprintf("http://app.test/requests?paid=%d", r.ID) || in.CancelURL != "http://app.test/requests" ||
		in.IdempotencyKey != fmt.Sprintf("merch-request-%d-pay", r.ID) {
		t.Fatalf("checkout input = %+v", in)
	}
	if ev := h.events("payment_started"); len(ev) != 1 || props(t, ev[0])["fee_cents"] != 1500.0 || props(t, ev[0])["country"] != "DE" {
		t.Fatalf("payment_started = %+v", ev)
	}

	// Clicking again reopens the same payment page instead of a second one.
	if _, loc := h.payLink(user, r); loc != "https://checkout.example/cs_test_1" || len(fs.created) != 1 {
		t.Fatalf("second click = %q, %d sessions", loc, len(fs.created))
	}

	// Paying moves the request to review as soon as they're back on My requests.
	fs.pay("cs_test_1", 1500)
	var mine struct{ Requests []viewed }
	h.do(user, "GET", "/api/requests", nil, &mine)
	got := mine.Requests[0]
	if got.Status != swag.StatusPending || got.PaidAt == nil || got.StripePaymentIntent != "pi_cs_test_1" || got.PaymentURL != "" {
		t.Fatalf("after paying = %+v", got)
	}
	if ev := h.events("payment_completed"); len(ev) != 1 || props(t, ev[0])["request_id"] != float64(r.ID) || ev[0].UserID == nil {
		t.Fatalf("payment_completed = %+v", ev)
	}
	h.do(user, "GET", "/api/requests", nil, &mine)
	if len(h.events("payment_completed")) != 1 {
		t.Fatal("payment recorded twice")
	}

	// An old pay link goes back to My requests.
	if code, loc := h.payLink(user, r); code != http.StatusSeeOther || loc != "/requests" {
		t.Fatalf("pay after paid = %d %q", code, loc)
	}
}

func TestCardPaymentSeenByAdminList(t *testing.T) {
	h, fs := newStripeHarness(t)
	r := h.intlRequest(userEmail)
	h.payLink(h.client(userEmail), r)
	fs.pay("cs_test_1", 1500)
	var all struct{ Requests []viewed }
	h.do(h.client(adminEmail), "GET", "/api/admin/requests", nil, &all)
	if all.Requests[0].Status != swag.StatusPending {
		t.Fatalf("admin sees %s", all.Requests[0].Status)
	}
	if ev := h.events("payment_completed"); len(ev) != 1 || *ev[0].UserID != r.UserID {
		t.Fatalf("payment_completed should be the requester's: %+v", ev)
	}
}

func TestCardPaymentWrongAmountIsNotAccepted(t *testing.T) {
	h, fs := newStripeHarness(t)
	r := h.intlRequest(userEmail)
	h.payLink(h.client(userEmail), r)
	fs.pay("cs_test_1", 100)
	got, _ := h.store.GetRequest(context.Background(), r.ID)
	var mine struct{ Requests []viewed }
	h.do(h.client(userEmail), "GET", "/api/requests", nil, &mine)
	if mine.Requests[0].Status != swag.StatusAwaitingPayment || got.PaidAt != nil {
		t.Fatalf("underpaid request = %+v", mine.Requests[0])
	}
}

func TestCancelClosesPaymentPage(t *testing.T) {
	h, fs := newStripeHarness(t)
	r := h.intlRequest(userEmail)
	h.payLink(h.client(userEmail), r)
	var got store.Request
	if code := h.do(h.client(userEmail), "POST", fmt.Sprintf("/api/requests/%d/cancel", r.ID), map[string]any{}, &got); code != 200 || got.Status != swag.StatusCancelled {
		t.Fatalf("cancel = %d %+v", code, got)
	}
	if len(fs.expired) != 1 || fs.expired[0] != "cs_test_1" {
		t.Fatalf("expired = %v", fs.expired)
	}
}

func TestUSRequestHasNoPaymentLink(t *testing.T) {
	h, _ := newStripeHarness(t)
	it := h.mkItem("Sti/A", true)
	var r viewed
	h.do(h.client(userEmail), "POST", "/api/requests", map[string]any{"purpose": "personal", "address": usAddr, "lines": []swag.Line{{ItemID: it.ID, Quantity: 1}}}, &r)
	if r.Status != swag.StatusPending || r.PaymentURL != "" {
		t.Fatalf("US request = %+v", r)
	}
}
