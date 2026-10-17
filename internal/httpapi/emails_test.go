package httpapi

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/hackclub/merch/internal/importer"
	"github.com/hackclub/merch/internal/loops"
	"github.com/hackclub/merch/internal/store"
	"github.com/hackclub/merch/internal/stripe"
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
	h.payByCard(userEmail, r.ID)
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

// paidIntlRequest is an international request paid by card, now pending.
func paidIntlRequest(t *testing.T, h *harness, fs *fakeStripe, email string) viewed {
	t.Helper()
	r := h.intlRequest(email)
	var pay payResponse
	h.do(h.client(email), "POST", fmt.Sprintf("/api/requests/%d/pay", r.ID), map[string]any{}, &pay)
	fs.set(pay.ClientSecret[:strings.Index(pay.ClientSecret, "_secret")], "succeeded", r.ShippingFeeCents)
	h.do(h.client(email), "GET", "/api/requests", nil, nil)
	return r
}

func newRefundHarness(t *testing.T) (*harness, *fakeStripe, *fakeMailer) {
	fs := &fakeStripe{}
	h, m := newMailHarness(t, func(d *Deps) { d.Payments = fs; d.Config.StripePublishableKey = "pk_test_x" })
	return h, fs, m
}

func TestRejectRefundsCardPayment(t *testing.T) {
	h, fs, m := newRefundHarness(t)
	r := paidIntlRequest(t, h, fs, userEmail)
	var got store.Request
	if code := h.do(h.client(adminEmail), "POST", fmt.Sprintf("/api/admin/requests/%d/reject", r.ID), map[string]any{"note": "Can't ship there."}, &got); code != 200 {
		t.Fatalf("reject = %d", code)
	}
	if len(fs.refunds) != 1 || fs.refunds[0] != (fakeRefund{"pi_1", 0}) || got.RefundedAt == nil || got.Status != swag.StatusRejected {
		t.Fatalf("refunds %v, request %+v", fs.refunds, got)
	}
	e := m.emails()
	mustContain(t, "rejected", e[len(e)-1].Body, "refunded", dollarsText(r.ShippingFeeCents), "Can't ship there.")
	if ev := h.events("payment_refunded"); len(ev) != 1 || props(t, ev[0])["by"] != "admin" || props(t, ev[0])["fee_cents"] != float64(r.ShippingFeeCents) {
		t.Fatalf("payment_refunded = %+v", ev)
	}
}

func dollarsText(cents int) string { return fmt.Sprintf("$%d.%02d", cents/100, cents%100) }

// If Stripe won't refund, the request stays as it was, so the admin can try again.
func TestRejectWaitsForRefund(t *testing.T) {
	h, fs, m := newRefundHarness(t)
	r := paidIntlRequest(t, h, fs, userEmail)
	fs.refundErr = fmt.Errorf("stripe down")
	if code := h.do(h.client(adminEmail), "POST", fmt.Sprintf("/api/admin/requests/%d/reject", r.ID), map[string]any{}, nil); code != 502 {
		t.Fatalf("reject = %d, want 502", code)
	}
	got, _ := h.store.GetRequest(context.Background(), r.ID)
	if got.Status != swag.StatusPending || len(m.emails()) != 1 {
		t.Fatalf("status %s, emails %d", got.Status, len(m.emails()))
	}
}

// Stripe can hold a refund for approval in its dashboard. The request stays
// open and the admin is told to approve it; once it has gone through, rejecting
// again finishes without refunding twice.
func TestRejectAfterRefundApproval(t *testing.T) {
	h, fs, m := newRefundHarness(t)
	r := paidIntlRequest(t, h, fs, userEmail)
	fs.refundErr = &stripe.ApprovalError{ID: "apreq_test", Message: "This action requires human approval"}
	var e struct{ Error string }
	if code := h.do(h.client(adminEmail), "POST", fmt.Sprintf("/api/admin/requests/%d/reject", r.ID), map[string]any{}, &e); code != 502 {
		t.Fatalf("reject = %d, want 502", code)
	}
	mustContain(t, "error", e.Error, "approve", "apreq_test", dollarsText(r.ShippingFeeCents), "reject it again")
	if got, _ := h.store.GetRequest(context.Background(), r.ID); got.Status != swag.StatusPending {
		t.Fatalf("status %s", got.Status)
	}

	// Approved in Stripe: the held refund ran by itself.
	fs.refundErr = nil
	fs.refunded = map[string]int{"pi_1": r.ShippingFeeCents}
	var got store.Request
	if code := h.do(h.client(adminEmail), "POST", fmt.Sprintf("/api/admin/requests/%d/reject", r.ID), map[string]any{}, &got); code != 200 {
		t.Fatalf("second reject = %d", code)
	}
	if got.Status != swag.StatusRejected || got.RefundedAt == nil || len(fs.refunds) != 0 {
		t.Fatalf("request %+v, refunds %v", got, fs.refunds)
	}
	em := m.emails()
	mustContain(t, "rejected", em[len(em)-1].Body, "refunded")
	if ev := h.events("payment_refunded"); len(ev) != 1 || props(t, ev[0])["existing"] != true {
		t.Fatalf("payment_refunded = %+v", ev)
	}
}

// Rejecting an unpaid request closes its payment, so it can't be paid afterwards.
func TestRejectClosesUnpaidPayment(t *testing.T) {
	h, fs, m := newRefundHarness(t)
	r := h.intlRequest(userEmail)
	h.do(h.client(userEmail), "POST", fmt.Sprintf("/api/requests/%d/pay", r.ID), map[string]any{}, nil)
	var got store.Request
	h.do(h.client(adminEmail), "POST", fmt.Sprintf("/api/admin/requests/%d/reject", r.ID), map[string]any{}, &got)
	if got.Status != swag.StatusRejected || len(fs.cancelled) != 1 || len(fs.refunds) != 0 || got.RefundedAt != nil {
		t.Fatalf("request %+v, cancelled %v, refunded %v", got, fs.cancelled, fs.refunds)
	}
	if e := m.emails(); len(e) != 1 || strings.Contains(e[0].Body, "refund") {
		t.Fatalf("emails = %+v", e)
	}
}

func TestCancelRefundsCardPayment(t *testing.T) {
	h, fs, _ := newRefundHarness(t)
	r := paidIntlRequest(t, h, fs, userEmail)
	var got store.Request
	if code := h.do(h.client(userEmail), "POST", fmt.Sprintf("/api/requests/%d/cancel", r.ID), map[string]any{}, &got); code != 200 {
		t.Fatalf("cancel = %d", code)
	}
	if got.Status != swag.StatusCancelled || got.RefundedAt == nil || len(fs.refunds) != 1 {
		t.Fatalf("request %+v, refunds %v", got, fs.refunds)
	}
	if ev := h.events("payment_refunded"); len(ev) != 1 || props(t, ev[0])["by"] != "requester" {
		t.Fatalf("payment_refunded = %+v", ev)
	}
}

// Shipping less than a paid international request asked for refunds the
// postage it no longer needs.
func TestShipLessRefundsPostageDifference(t *testing.T) {
	h, fs, m := newRefundHarness(t)
	_, _, magazine := shippingFixture(t, h)
	var csv strings.Builder
	for i := range 12 {
		csv.WriteString(fmt.Sprintf("zenventory,DE,parcel,13,900,2026-09-01,2026-09-02,2026-09-1%d,\n", 2+i%5))
		csv.WriteString(fmt.Sprintf("zenventory,DE,parcel,34,2400,2026-09-01,2026-09-02,2026-09-1%d,\n", 2+i%5))
	}
	ships, err := importer.ParseShipments(strings.NewReader(csv.String()))
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.ReplaceShippingHistory(context.Background(), ships); err != nil {
		t.Fatal(err)
	}
	user := h.client(userEmail)
	var r viewed
	h.do(user, "POST", "/api/requests", map[string]any{"purpose": "club", "note": "for our club", "customs_ok": true, "address": deAddr,
		"lines": []swag.Line{{ItemID: magazine.ID, Quantity: 3}}}, &r)
	var one struct{ Estimate swag.Estimate }
	h.do(user, "POST", "/api/shipping/estimate", map[string]any{"country": "DE", "lines": []swag.Line{{ItemID: magazine.ID, Quantity: 1}}}, &one)
	if one.Estimate.FeeCents >= r.ShippingFeeCents {
		t.Fatalf("fixture: 1 magazine costs %d, 3 cost %d", one.Estimate.FeeCents, r.ShippingFeeCents)
	}
	var pay payResponse
	h.do(user, "POST", fmt.Sprintf("/api/requests/%d/pay", r.ID), map[string]any{}, &pay)
	fs.set("pi_1", "succeeded", r.ShippingFeeCents)
	h.do(user, "GET", "/api/requests", nil, nil)

	var got store.Request
	if code := h.do(h.client(adminEmail), "POST", fmt.Sprintf("/api/admin/requests/%d/dispatch", r.ID),
		map[string]any{"lines": []map[string]any{{"sku": magazine.SKU, "quantity": 1}}}, &got); code != 200 {
		t.Fatalf("dispatch = %d", code)
	}
	diff := r.ShippingFeeCents - one.Estimate.FeeCents
	if len(fs.refunds) != 1 || fs.refunds[0] != (fakeRefund{"pi_1", diff}) || got.RefundedCents != diff || got.RefundedAt == nil {
		t.Fatalf("refunds %+v, request refunded %d at %v, want %d", fs.refunds, got.RefundedCents, got.RefundedAt, diff)
	}
	e := m.emails()
	mustContain(t, "shipped", e[len(e)-1].Body, "refunded "+dollarsText(diff))
	if ev := h.events("payment_refunded"); len(ev) != 1 || props(t, ev[0])["refund_cents"] != float64(diff) {
		t.Fatalf("payment_refunded = %+v", ev)
	}
}

// Every email is saved word for word (sent_emails), and its usage event points at the row.
func TestEmailsAreLogged(t *testing.T) {
	h, m := newMailHarness(t)
	it := h.mkItem("Sti/A", true)
	var r store.Request
	h.do(h.client(userEmail), "POST", "/api/requests", map[string]any{"purpose": "personal", "address": usAddr, "lines": []swag.Line{{ItemID: it.ID, Quantity: 2}}}, &r)
	m.fail = fmt.Errorf("loops: 500: down")
	h.do(h.client(adminEmail), "POST", fmt.Sprintf("/api/admin/requests/%d/dispatch", r.ID), map[string]any{}, nil)

	logged, err := h.store.ListEmails(context.Background(), store.EmailFilter{RequestID: r.ID, Limit: 10})
	if err != nil || len(logged) != 2 {
		t.Fatalf("logged = %+v %v", logged, err)
	}
	failed, sent := logged[0], logged[1]
	want := m.emails()[0]
	if sent.Status != "sent" || sent.Kind != "received" || sent.To != userEmail || sent.Subject != want.Subject ||
		sent.Preview != want.Preview || sent.Body != want.Body || sent.IdempotencyKey != want.IdempotencyKey || sent.UserID == nil || *sent.UserID != r.UserID {
		t.Fatalf("sent row = %+v, email %+v", sent, want)
	}
	if failed.Status != "failed" || failed.Kind != "dispatched" || failed.Error != "loops: 500: down" || !strings.Contains(failed.Body, "2 × Sti/A") {
		t.Fatalf("failed row = %+v", failed)
	}
	if e := h.events("email_sent"); len(e) != 1 || props(t, e[0])["email_id"] != float64(sent.ID) {
		t.Fatalf("email_sent = %+v", e)
	}
	if e := h.events("email_failed"); len(e) != 1 || props(t, e[0])["email_id"] != float64(failed.ID) {
		t.Fatalf("email_failed = %+v", e)
	}

	// Admins and review agents can read them; nobody else can.
	var out struct {
		Emails []store.SentEmail `json:"emails"`
	}
	if code := h.do(h.client(adminEmail), "GET", "/api/admin/emails", nil, &out); code != 200 || len(out.Emails) != 2 || out.Emails[0].ID != failed.ID {
		t.Fatalf("admin emails = %d %+v", code, out)
	}
	if code := h.do(h.client(adminEmail), "GET", "/api/admin/emails?status=failed", nil, &out); code != 200 || len(out.Emails) != 1 {
		t.Fatalf("admin failed emails = %d %+v", code, out)
	}
	if code := h.do(h.client(userEmail), "GET", "/api/admin/emails", nil, nil); code != 403 {
		t.Fatalf("user reads emails = %d", code)
	}
	agent := h.keyClient(h.mkKey("Review agent").Token)
	if code := h.do(agent, "GET", fmt.Sprintf("/api/agent/emails?request_id=%d", r.ID), nil, &out); code != 200 || len(out.Emails) != 2 {
		t.Fatalf("agent emails = %d %+v", code, out)
	}
	var detail struct {
		Emails []store.SentEmail `json:"emails"`
	}
	if code := h.do(agent, "GET", fmt.Sprintf("/api/agent/requests/%d", r.ID), nil, &detail); code != 200 || len(detail.Emails) != 2 {
		t.Fatalf("agent request emails = %d %+v", code, detail)
	}
}

// Without Loops (dev), emails are still logged as unsent so their wording can be reviewed.
func TestEmailsLoggedWithoutMailer(t *testing.T) {
	h := newHarness(t)
	it := h.mkItem("Sti/A", true)
	var r store.Request
	h.do(h.client(userEmail), "POST", "/api/requests", map[string]any{"purpose": "personal", "address": usAddr, "lines": []swag.Line{{ItemID: it.ID, Quantity: 1}}}, &r)
	logged, err := h.store.ListEmails(context.Background(), store.EmailFilter{RequestID: r.ID, Limit: 10})
	if err != nil || len(logged) != 1 || logged[0].Status != "unsent" || !strings.Contains(logged[0].Body, "1 × Sti/A") {
		t.Fatalf("logged = %+v %v", logged, err)
	}
	if len(h.events("email_sent")) != 0 {
		t.Fatal("unsent email tracked as sent")
	}
}

func TestShipAddsItems(t *testing.T) {
	h, m := newMailHarness(t)
	a, b := h.mkItem("Sti/A", true), h.mkItem("Pri/B", true)
	h.mkItem("Sti/Hidden", false)
	if _, err := h.store.CreateItem(t.Context(), swag.Item{SKU: "Swa/S", Name: "Shirt", Visible: true, MaxPerRequest: 3,
		Variants: []swag.Variant{{Label: "M", SKU: "Swa/S/M"}}}); err != nil {
		t.Fatal(err)
	}
	admin := h.client(adminEmail)
	var r store.Request
	h.do(h.client(userEmail), "POST", "/api/requests", map[string]any{"purpose": "personal", "address": usAddr,
		"lines": []swag.Line{{ItemID: a.ID, Quantity: 1}}}, &r)
	path := fmt.Sprintf("/api/admin/requests/%d/dispatch", r.ID)

	for name, body := range map[string]map[string]any{
		"hidden item":      {"lines": []map[string]any{{"sku": "Sti/Hidden", "quantity": 1}}},
		"sized item":       {"lines": []map[string]any{{"sku": "Swa/S/M", "quantity": 1}}},
		"over its maximum": {"lines": []map[string]any{{"sku": "Pri/B", "quantity": 4}}},
	} {
		if code := h.do(admin, "POST", path, body, nil); code != 422 {
			t.Errorf("%s = %d, want 422", name, code)
		}
	}

	var got store.Request
	code := h.do(admin, "POST", path, map[string]any{"lines": []map[string]any{{"sku": "Sti/A", "quantity": 2}, {"sku": "Pri/B", "quantity": 3}}}, &got)
	if code != 200 || len(got.Lines) != 2 {
		t.Fatalf("dispatch = %d %+v", code, got)
	}
	for _, l := range got.Lines {
		if l.ItemID == b.ID && (l.Quantity != 3 || l.RequestedQuantity == nil || *l.RequestedQuantity != 0) {
			t.Errorf("added line = %+v", l)
		}
	}
	if c := h.wh.orders[0].Contents; len(c) != 2 {
		t.Fatalf("contents = %+v", c)
	}
	e := m.emails()[1]
	mustContain(t, "shipped", e.Body, "- 2 × Sti/A", "- 3 × Pri/B", "Sti/A: you asked for 1, we sent 2", "Pri/B: we added 3")
}
