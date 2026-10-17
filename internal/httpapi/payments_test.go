package httpapi

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/hackclub/merch/internal/store"
	"github.com/hackclub/merch/internal/stripe"
	"github.com/hackclub/merch/internal/swag"
)

// fakeStripe stands in for Stripe's PaymentIntents.
type fakeStripe struct {
	mu        sync.Mutex
	payments  map[string]*stripe.Payment
	created   []stripe.PaymentInput
	cancelled []string
	refunds   []fakeRefund
	refundErr error
}

type fakeRefund struct {
	id     string
	amount int // 0 = all of it
}

func (f *fakeStripe) Refund(_ context.Context, id string, amountCents int, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.refundErr != nil {
		return f.refundErr
	}
	f.refunds = append(f.refunds, fakeRefund{id, amountCents})
	return nil
}

func (f *fakeStripe) CreatePayment(_ context.Context, in stripe.PaymentInput) (*stripe.Payment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, in)
	id := fmt.Sprintf("pi_%d", len(f.created))
	p := &stripe.Payment{ID: id, ClientSecret: id + "_secret", Status: "requires_payment_method", Amount: in.AmountCents, Currency: "usd",
		Metadata: map[string]string{"request_id": strconv.FormatInt(in.RequestID, 10)}}
	if f.payments == nil {
		f.payments = map[string]*stripe.Payment{}
	}
	f.payments[id] = p
	cp := *p
	return &cp, nil
}

func (f *fakeStripe) GetPayment(_ context.Context, id string) (*stripe.Payment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.payments[id]
	if !ok {
		return nil, fmt.Errorf("stripe: 404: no such payment")
	}
	cp := *p
	return &cp, nil
}

func (f *fakeStripe) CancelPayment(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancelled = append(f.cancelled, id)
	f.payments[id].Status = "canceled"
	return nil
}

// set changes a payment as if the browser confirmed a card: status, and what was received.
func (f *fakeStripe) set(id, status string, received int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.payments[id].Status, f.payments[id].AmountReceived = status, received
}

func newStripeHarness(t *testing.T) (*harness, *fakeStripe) {
	fs := &fakeStripe{}
	h := newHarness(t, func(d *Deps) {
		d.Payments = fs
		d.Config.StripePublishableKey = "pk_test_x"
	})
	return h, fs
}

type viewed struct {
	store.Request
	StripeKey string `json:"stripe_key"`
}

type payResponse struct {
	ClientSecret string `json:"client_secret"`
	AmountCents  int    `json:"amount_cents"`
	Error        string `json:"error"`
}

func (h *harness) intlRequest(email string) viewed {
	h.t.Helper()
	it := h.mkItem(fmt.Sprintf("Sti/%s", email), true)
	var r viewed
	if code := h.do(h.client(email), "POST", "/api/requests", map[string]any{"purpose": "personal", "customs_ok": true, "address": deAddr, "lines": []swag.Line{{ItemID: it.ID, Quantity: 1}}}, &r); code != 201 {
		h.t.Fatalf("create = %d", code)
	}
	return r
}

func (h *harness) pay(email string, id int64) (int, payResponse) {
	h.t.Helper()
	var out payResponse
	code := h.do(h.client(email), "POST", fmt.Sprintf("/api/requests/%d/pay", id), map[string]any{}, &out)
	return code, out
}

// payByCard pays a request's postage with the harness's fake Stripe, as the
// card form would, and returns the request once the app has seen the payment.
func (h *harness) payByCard(email string, id int64) store.Request {
	h.t.Helper()
	code, p := h.pay(email, id)
	if code != 200 {
		h.t.Fatalf("pay = %d %+v", code, p)
	}
	pi := p.ClientSecret[:strings.Index(p.ClientSecret, "_secret")]
	h.stripe.set(pi, "succeeded", p.AmountCents)
	var out struct {
		Requests []store.Request `json:"requests"`
	}
	h.do(h.client(email), "GET", "/api/requests", nil, &out)
	for _, r := range out.Requests {
		if r.ID == id {
			return r
		}
	}
	h.t.Fatalf("request %d not listed", id)
	return store.Request{}
}

func TestCardPaymentForInternationalShipping(t *testing.T) {
	h, fs := newStripeHarness(t)
	r := h.intlRequest(userEmail)
	if r.Status != swag.StatusAwaitingPayment || r.StripeKey != "pk_test_x" {
		t.Fatalf("created = %+v", r)
	}
	var cat struct {
		Settings struct {
			StripeKey string `json:"stripe_key"`
		}
	}
	h.do(h.client(""), "GET", "/api/catalog", nil, &cat)
	if cat.Settings.StripeKey != "pk_test_x" {
		t.Fatalf("catalog stripe_key = %q", cat.Settings.StripeKey)
	}

	// Only the requester can pay.
	if code, _ := h.pay(authorEmail, r.ID); code != 404 {
		t.Fatalf("someone else's pay = %d", code)
	}

	code, p := h.pay(userEmail, r.ID)
	if code != 200 || p.ClientSecret != "pi_1_secret" || p.AmountCents != 1500 {
		t.Fatalf("pay = %d %+v", code, p)
	}
	in := fs.created[0]
	if in.RequestID != r.ID || in.AmountCents != 1500 || in.Email != userEmail || in.IdempotencyKey != fmt.Sprintf("merch-request-%d-pay", r.ID) {
		t.Fatalf("payment input = %+v", in)
	}
	if !strings.Contains(in.Description, "Items requested:\n- ") || !strings.Contains(in.Description, "Total postage paid: $15.00 USD") {
		t.Fatalf("receipt description = %q", in.Description)
	}
	if ev := h.events("payment_started"); len(ev) != 1 || props(t, ev[0])["fee_cents"] != 1500.0 || props(t, ev[0])["country"] != "DE" {
		t.Fatalf("payment_started = %+v", ev)
	}

	// A declined card leaves the payment open: trying again reuses it.
	if _, p := h.pay(userEmail, r.ID); p.ClientSecret != "pi_1_secret" || len(fs.created) != 1 {
		t.Fatalf("retry = %+v, %d payments", p, len(fs.created))
	}

	// Once the card goes through, the request moves to review the next time it's looked at.
	fs.set("pi_1", "succeeded", 1500)
	var mine struct{ Requests []viewed }
	h.do(h.client(userEmail), "GET", "/api/requests", nil, &mine)
	got := mine.Requests[0]
	if got.Status != swag.StatusPending || got.PaidAt == nil || got.StripePaymentIntent != "pi_1" || got.StripeKey != "" {
		t.Fatalf("after paying = %+v", got)
	}
	if ev := h.events("payment_completed"); len(ev) != 1 || props(t, ev[0])["request_id"] != float64(r.ID) || ev[0].UserID == nil {
		t.Fatalf("payment_completed = %+v", ev)
	}
	h.do(h.client(userEmail), "GET", "/api/requests", nil, &mine)
	if len(h.events("payment_completed")) != 1 {
		t.Fatal("payment recorded twice")
	}
	if code, p := h.pay(userEmail, r.ID); code != 409 || p.Error == "" {
		t.Fatalf("pay after paid = %d %+v", code, p)
	}
}

func TestPayChecksStripeFirst(t *testing.T) {
	h, fs := newStripeHarness(t)
	r := h.intlRequest(userEmail)
	h.pay(userEmail, r.ID)

	// Paid but not yet seen: pay records it instead of charging again.
	fs.set("pi_1", "succeeded", 1500)
	if code, _ := h.pay(userEmail, r.ID); code != 409 || len(fs.created) != 1 {
		t.Fatalf("pay when already paid = %d, %d payments", code, len(fs.created))
	}
	if got, _ := h.store.GetRequest(context.Background(), r.ID); got.Status != swag.StatusPending {
		t.Fatalf("status = %s", got.Status)
	}
}

func TestPaymentStillProcessing(t *testing.T) {
	h, fs := newStripeHarness(t)
	r := h.intlRequest(userEmail)
	h.pay(userEmail, r.ID)
	fs.set("pi_1", "processing", 0)
	if code, p := h.pay(userEmail, r.ID); code != 409 || len(fs.created) != 1 {
		t.Fatalf("pay while processing = %d %+v, %d payments", code, p, len(fs.created))
	}
}

func TestCancelledPaymentStartsANewOne(t *testing.T) {
	h, fs := newStripeHarness(t)
	r := h.intlRequest(userEmail)
	h.pay(userEmail, r.ID)
	fs.set("pi_1", "canceled", 0)
	code, p := h.pay(userEmail, r.ID)
	if code != 200 || p.ClientSecret != "pi_2_secret" || fs.created[1].IdempotencyKey != fmt.Sprintf("merch-request-%d-pay-after-pi_1", r.ID) {
		t.Fatalf("pay after cancel = %d %+v %+v", code, p, fs.created)
	}
}

func TestCardPaymentSeenByAdminList(t *testing.T) {
	h, fs := newStripeHarness(t)
	r := h.intlRequest(userEmail)
	h.pay(userEmail, r.ID)
	fs.set("pi_1", "succeeded", 1500)
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
	h.pay(userEmail, r.ID)
	fs.set("pi_1", "succeeded", 100)
	var mine struct{ Requests []viewed }
	h.do(h.client(userEmail), "GET", "/api/requests", nil, &mine)
	if mine.Requests[0].Status != swag.StatusAwaitingPayment || mine.Requests[0].PaidAt != nil {
		t.Fatalf("underpaid request = %+v", mine.Requests[0])
	}
}

func TestCancelStopsPayment(t *testing.T) {
	h, fs := newStripeHarness(t)
	r := h.intlRequest(userEmail)
	h.pay(userEmail, r.ID)
	var got store.Request
	if code := h.do(h.client(userEmail), "POST", fmt.Sprintf("/api/requests/%d/cancel", r.ID), map[string]any{}, &got); code != 200 || got.Status != swag.StatusCancelled {
		t.Fatalf("cancel = %d %+v", code, got)
	}
	if len(fs.cancelled) != 1 || fs.cancelled[0] != "pi_1" {
		t.Fatalf("cancelled = %v", fs.cancelled)
	}
}

func TestUSRequestHasNothingToPay(t *testing.T) {
	h, _ := newStripeHarness(t)
	it := h.mkItem("Sti/A", true)
	var r viewed
	h.do(h.client(userEmail), "POST", "/api/requests", map[string]any{"purpose": "personal", "address": usAddr, "lines": []swag.Line{{ItemID: it.ID, Quantity: 1}}}, &r)
	if r.Status != swag.StatusPending || r.StripeKey != "" {
		t.Fatalf("US request = %+v", r)
	}
	if code, _ := h.pay(userEmail, r.ID); code != 409 {
		t.Fatalf("pay US request = %d", code)
	}
}

func TestPaymentDescription(t *testing.T) {
	req := &store.Request{ID: 7, ShippingFeeCents: 1925,
		Address: swag.Address{City: "Toronto", State: "ON", Country: "CA", Line1: "1 Secret St"},
		Lines: []store.RequestLine{
			{Name: "Mini-Magazine", Quantity: 100},
			{Name: "Hack Club T-shirt", Variant: "M", Quantity: 1},
		}}
	want := "Postage for Hack Club swag request #7\n" +
		"Ships to: Toronto, ON, CA\n" +
		"Items requested:\n" +
		"- 100 × Mini-Magazine\n" +
		"- 1 × Hack Club T-shirt (M)\n" +
		"Total postage paid: $19.25 USD\n" +
		"The swag is free; this pays the postage to send it outside the US.\n" +
		"See your request: https://merch.example/requests"
	if got := paymentDescription(req, "https://merch.example"); got != want {
		t.Fatalf("description =\n%s\nwant\n%s", got, want)
	}

	// Stripe refuses descriptions over 1000 characters: a long request lists
	// what fits and counts the rest.
	req.Lines = nil
	for i := 0; i < 60; i++ {
		req.Lines = append(req.Lines, store.RequestLine{Name: fmt.Sprintf("Sticker number %d with a long name", i), Quantity: 2})
	}
	got := paymentDescription(req, "https://merch.example")
	if n := len([]rune(got)); n > 1000 {
		t.Fatalf("description is %d characters", n)
	}
	if !strings.Contains(got, "more items\nTotal postage paid: $19.25 USD") || !strings.HasPrefix(got, "Postage for Hack Club swag request #7\n") {
		t.Fatalf("long description =\n%s", got)
	}
}
