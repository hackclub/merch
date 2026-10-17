package httpapi

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/hackclub/merch/internal/loops"
	"github.com/hackclub/merch/internal/store"
	"github.com/hackclub/merch/internal/swag"
)

// fakeMailer stands in for Loops.
type fakeMailer struct {
	mu   sync.Mutex
	sent []loops.Email
	fail error
}

func (f *fakeMailer) Send(_ context.Context, e loops.Email) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	f.sent = append(f.sent, e)
	return nil
}

func (f *fakeMailer) emails() []loops.Email {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]loops.Email(nil), f.sent...)
}

func newMailHarness(t *testing.T, opts ...func(*Deps)) (*harness, *fakeMailer) {
	m := &fakeMailer{}
	h := newHarness(t, append([]func(*Deps){func(d *Deps) { d.Mailer = m }}, opts...)...)
	return h, m
}

func mustContain(t *testing.T, what, s string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(s, w) {
			t.Errorf("%s doesn't say %q:\n%s", what, w, s)
		}
	}
}

func TestReceivedEmailText(t *testing.T) {
	req := &store.Request{ID: 12, UserEmail: "orpheus@hackclub.com", Address: usAddr, Lines: []store.RequestLine{
		{Name: "Sticker", Quantity: 10}, {Name: "T-shirt", Variant: "M", Quantity: 1},
	}}
	e := receivedEmail(req, "https://merch.hackclub.com")
	if e.To != "orpheus@hackclub.com" || e.IdempotencyKey != "" {
		t.Fatalf("email = %+v", e)
	}
	mustContain(t, "subject", e.Subject, "request")
	mustContain(t, "body", e.Body, "Hi O,", "10 × Sticker", "1 × T-shirt (M)", "Burlington, VT, US", "https://merch.hackclub.com/requests")
	if strings.Contains(e.Body, "customs") {
		t.Errorf("US email mentions customs:\n%s", e.Body)
	}

	intl := *req
	intl.Address, intl.ShippingFeeCents = deAddr, 1200
	e = receivedEmail(&intl, "https://merch.hackclub.com")
	mustContain(t, "intl body", e.Body, "$12.00", "customs", "Berlin, DE")
}

func TestShippedAndRejectedEmailText(t *testing.T) {
	req := &store.Request{ID: 12, UserEmail: "orpheus@hackclub.com", Address: usAddr, Lines: []store.RequestLine{{Name: "Sticker", Quantity: 10}}}
	e := shippedEmail(req, "https://merch.hackclub.com")
	mustContain(t, "shipped subject", e.Subject, "on its way")
	mustContain(t, "shipped body", e.Body, "10 × Sticker", "tracking")

	req.AdminNote = "Out of the red ones, so we sent blue."
	mustContain(t, "shipped body with note", shippedEmail(req, "https://x").Body, "Out of the red ones")

	req.AdminNote = ""
	e = rejectedEmail(req, "https://merch.hackclub.com")
	mustContain(t, "rejected subject", e.Subject, "can't send")
	if strings.Contains(e.Body, "Why:") {
		t.Errorf("rejected email without a reason gives one:\n%s", e.Body)
	}
	req.AdminNote = "We're out of stickers."
	mustContain(t, "rejected body", rejectedEmail(req, "https://x").Body, "We're out of stickers.")
}

func TestRequestEmails(t *testing.T) {
	h, m := newMailHarness(t)
	it := h.mkItem("Sti/A", true)
	user, admin := h.client(userEmail), h.client(adminEmail)

	var r store.Request
	h.do(user, "POST", "/api/requests", map[string]any{"purpose": "personal", "address": usAddr, "lines": []swag.Line{{ItemID: it.ID, Quantity: 2}}}, &r)
	got := m.emails()
	if len(got) != 1 || got[0].To != userEmail || got[0].IdempotencyKey != fmt.Sprintf("merch-request-%d-received", r.ID) {
		t.Fatalf("after create: %+v", got)
	}
	mustContain(t, "received", got[0].Body, "2 × Sti/A")

	if code := h.do(admin, "POST", fmt.Sprintf("/api/admin/requests/%d/dispatch", r.ID), map[string]any{}, nil); code != 200 {
		t.Fatalf("dispatch = %d", code)
	}
	got = m.emails()
	if len(got) != 2 || got[1].IdempotencyKey != fmt.Sprintf("merch-request-%d-dispatched", r.ID) {
		t.Fatalf("after dispatch: %+v", got)
	}

	var r2 store.Request
	h.do(h.client(authorEmail), "POST", "/api/requests", map[string]any{"purpose": "personal", "address": usAddr, "lines": []swag.Line{{ItemID: it.ID, Quantity: 1}}}, &r2)
	if code := h.do(admin, "POST", fmt.Sprintf("/api/admin/requests/%d/reject", r2.ID), map[string]any{"note": "We're out."}, nil); code != 200 {
		t.Fatalf("reject = %d", code)
	}
	got = m.emails()
	if len(got) != 4 || got[3].To != authorEmail || got[3].IdempotencyKey != fmt.Sprintf("merch-request-%d-rejected", r2.ID) {
		t.Fatalf("after reject: %+v", got)
	}
	mustContain(t, "rejected", got[3].Body, "We're out.")

	sent := h.events("email_sent")
	if len(sent) != 4 || props(t, sent[3])["kind"] != "rejected" || props(t, sent[3])["request_id"] != float64(r2.ID) {
		t.Fatalf("email_sent events = %+v", sent)
	}
}

// An international request is confirmed once its postage is paid, not before.
func TestInternationalReceivedEmailWaitsForPayment(t *testing.T) {
	h, m := newMailHarness(t)
	it := h.mkItem("Sti/A", true)
	var r store.Request
	h.do(h.client(userEmail), "POST", "/api/requests", map[string]any{"purpose": "personal", "customs_ok": true, "address": deAddr, "lines": []swag.Line{{ItemID: it.ID, Quantity: 1}}}, &r)
	if r.Status != swag.StatusAwaitingPayment || len(m.emails()) != 0 {
		t.Fatalf("status %s, emails %+v", r.Status, m.emails())
	}
	h.do(h.client(adminEmail), "POST", fmt.Sprintf("/api/admin/requests/%d/mark-paid", r.ID), map[string]any{}, nil)
	if got := m.emails(); len(got) != 1 || !strings.Contains(got[0].Body, "customs") {
		t.Fatalf("after paid: %+v", got)
	}
}

func TestCardPaymentSendsReceivedEmail(t *testing.T) {
	fs := &fakeStripe{}
	h, m := newMailHarness(t, func(d *Deps) { d.Payments = fs; d.Config.StripePublishableKey = "pk_test_x" })
	r := h.intlRequest(userEmail)
	var pay payResponse
	h.do(h.client(userEmail), "POST", fmt.Sprintf("/api/requests/%d/pay", r.ID), map[string]any{}, &pay)
	fs.set("pi_1", "succeeded", r.ShippingFeeCents)
	h.do(h.client(userEmail), "GET", "/api/requests", nil, nil)
	h.do(h.client(userEmail), "GET", "/api/requests", nil, nil)
	if got := m.emails(); len(got) != 1 || got[0].IdempotencyKey != fmt.Sprintf("merch-request-%d-received", r.ID) {
		t.Fatalf("emails = %+v", got)
	}
}

// A failed email is logged; the action still happens.
func TestEmailFailureDoesntBlockShipping(t *testing.T) {
	h, m := newMailHarness(t)
	m.fail = fmt.Errorf("loops down")
	it := h.mkItem("Sti/A", true)
	var r store.Request
	if code := h.do(h.client(userEmail), "POST", "/api/requests", map[string]any{"purpose": "personal", "address": usAddr, "lines": []swag.Line{{ItemID: it.ID, Quantity: 1}}}, &r); code != 201 {
		t.Fatalf("create = %d", code)
	}
	if code := h.do(h.client(adminEmail), "POST", fmt.Sprintf("/api/admin/requests/%d/dispatch", r.ID), map[string]any{}, nil); code != 200 {
		t.Fatalf("dispatch = %d", code)
	}
	if f := h.events("email_failed"); len(f) != 2 || props(t, f[0])["kind"] != "received" {
		t.Fatalf("email_failed = %+v", f)
	}
}

func TestInternationalRequestNeedsCustomsOK(t *testing.T) {
	h := newHarness(t)
	it := h.mkItem("Sti/A", true)
	var errBody map[string]string
	code := h.do(h.client(userEmail), "POST", "/api/requests", map[string]any{"purpose": "personal", "address": deAddr, "lines": []swag.Line{{ItemID: it.ID, Quantity: 1}}}, &errBody)
	if code != 422 || !strings.Contains(errBody["error"], "customs") {
		t.Fatalf("no customs_ok = %d %v", code, errBody)
	}
	if e := h.events("request_refused"); len(e) != 1 || !strings.Contains(props(t, e[0])["error"].(string), "customs") {
		t.Fatalf("request_refused = %+v", e)
	}
}

func TestShipWithChanges(t *testing.T) {
	h, m := newMailHarness(t)
	a, b := h.mkItem("Sti/A", true), h.mkItem("Sti/B", true)
	admin := h.client(adminEmail)
	var r store.Request
	h.do(h.client(userEmail), "POST", "/api/requests", map[string]any{"purpose": "personal", "address": usAddr,
		"lines": []swag.Line{{ItemID: a.ID, Quantity: 3}, {ItemID: b.ID, Quantity: 1}}}, &r)
	path := fmt.Sprintf("/api/admin/requests/%d/dispatch", r.ID)

	for name, body := range map[string]map[string]any{
		"unknown sku": {"lines": []map[string]any{{"sku": "Sti/Z", "quantity": 1}}},
		"negative":    {"lines": []map[string]any{{"sku": "Sti/A", "quantity": -1}}},
		"nothing":     {"lines": []map[string]any{{"sku": "Sti/A", "quantity": 0}, {"sku": "Sti/B", "quantity": 0}}},
	} {
		if code := h.do(admin, "POST", path, body, nil); code != 422 {
			t.Errorf("%s = %d, want 422", name, code)
		}
	}
	if len(h.wh.orders) != 0 {
		t.Fatalf("refused changes still ordered: %+v", h.wh.orders)
	}

	var got store.Request
	code := h.do(admin, "POST", path, map[string]any{"lines": []map[string]any{{"sku": "Sti/A", "quantity": 2}, {"sku": "Sti/B", "quantity": 0}}, "note": "We're out of B."}, &got)
	if code != 200 || got.AdminNote != "We're out of B." {
		t.Fatalf("dispatch = %d %+v", code, got)
	}
	if c := h.wh.orders[0].Contents; len(c) != 1 || c[0].SKU != "Sti/A" || c[0].Quantity != 2 {
		t.Fatalf("contents = %+v", c)
	}
	e := m.emails()[1]
	mustContain(t, "shipped", e.Body, "- 2 × Sti/A", "Sti/A: you asked for 3, we sent 2", "Sti/B: you asked for 1, we couldn't send it", "We're out of B.")
	if strings.Contains(e.Body, "× Sti/B") {
		t.Errorf("removed line listed as shipped:\n%s", e.Body)
	}
}
