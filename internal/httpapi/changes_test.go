package httpapi

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hackclub/merch/internal/store"
	"github.com/hackclub/merch/internal/swag"
	"github.com/hackclub/merch/internal/theseus"
)

// When the warehouse runs short of something already sent to it, AGH changes
// the order and we record the cut here once they confirm it: the request keeps
// what was asked for, the requester gets an email saying what changed.
// confirmer is who said the warehouse made the change (made up).
const confirmer = "Orpheus at the warehouse"

func TestReduceDispatchedRequest(t *testing.T) {
	h, m := newMailHarness(t)
	pin, card := h.mkItem("Sti/A", true), h.mkItem("Sti/B", true)
	pin.MaxPerRequest = 5
	if _, err := h.store.UpdateItem(context.Background(), pin); err != nil {
		t.Fatal(err)
	}
	admin := h.client(adminEmail)
	mk := func(email string, pins int) store.Request {
		var r store.Request
		if code := h.do(h.client(email), "POST", "/api/requests", map[string]any{"purpose": "personal", "address": usAddr,
			"lines": []swag.Line{{ItemID: pin.ID, Quantity: pins}, {ItemID: card.ID, Quantity: 2}}}, &r); code != 201 {
			t.Fatalf("create = %d", code)
		}
		return r
	}
	r := mk(userEmail, 5)
	path := fmt.Sprintf("/api/admin/requests/%d/lines", r.ID)
	cut := map[string]any{"lines": []map[string]any{{"sku": "Sti/A", "quantity": 1}}, "note": "Our pins ran out.", "warehouse_confirmed_by": confirmer}

	if code := h.do(admin, "POST", path, cut, nil); code != 409 {
		t.Fatalf("pending request = %d, want 409", code)
	}
	if code := h.do(admin, "POST", fmt.Sprintf("/api/admin/requests/%d/dispatch", r.ID), map[string]any{}, nil); code != 200 {
		t.Fatalf("dispatch = %d", code)
	}
	if code := h.do(h.client(userEmail), "POST", path, cut, nil); code != 403 {
		t.Fatalf("non-admin = %d, want 403", code)
	}
	// Nothing is recorded until someone says the warehouse made the change:
	// editing an order AGH already has, from our side, ships the wrong box.
	sent := len(m.emails())
	for name, who := range map[string]any{"missing": nil, "blank": "  ", "too long": strings.Repeat("x", 201)} {
		body := map[string]any{"lines": []map[string]any{{"sku": "Sti/A", "quantity": 1}}, "note": "Our pins ran out."}
		if who != nil {
			body["warehouse_confirmed_by"] = who
		}
		if code := h.do(admin, "POST", path, body, nil); code != 422 {
			t.Errorf("unconfirmed (%s) = %d, want 422", name, code)
		}
	}
	if n := len(m.emails()); n != sent {
		t.Fatalf("unconfirmed cut sent %d emails", n-sent)
	}
	if ev := h.events("request_changed"); len(ev) != 0 {
		t.Fatalf("unconfirmed cut tracked: %+v", ev)
	}
	for name, body := range map[string]map[string]any{
		"more than asked": {"lines": []map[string]any{{"sku": "Sti/A", "quantity": 6}}, "warehouse_confirmed_by": confirmer},
		"negative":        {"lines": []map[string]any{{"sku": "Sti/A", "quantity": -1}}, "warehouse_confirmed_by": confirmer},
		"unknown sku":     {"lines": []map[string]any{{"sku": "Sti/Z", "quantity": 0}}, "warehouse_confirmed_by": confirmer},
		"no change":       {"lines": []map[string]any{{"sku": "Sti/A", "quantity": 5}}, "warehouse_confirmed_by": confirmer},
		"nothing":         {"lines": []map[string]any{{"sku": "Sti/A", "quantity": 0}, {"sku": "Sti/B", "quantity": 0}}, "warehouse_confirmed_by": confirmer},
	} {
		if code := h.do(admin, "POST", path, body, nil); code != 422 {
			t.Errorf("%s = %d, want 422", name, code)
		}
	}

	var got store.Request
	if code := h.do(admin, "POST", path, cut, &got); code != 200 {
		t.Fatalf("cut = %d", code)
	}
	for _, l := range got.Lines {
		if l.SKU == "Sti/A" && (l.Quantity != 1 || l.RequestedQuantity == nil || *l.RequestedQuantity != 5) {
			t.Fatalf("pin line = %+v", l)
		}
	}
	if got.AdminNote != "Our pins ran out." || got.Status != swag.StatusDispatched {
		t.Fatalf("request = %+v", got)
	}
	if !strings.Contains(got.InternalNote, "Warehouse change confirmed by "+confirmer) {
		t.Fatalf("internal note = %q", got.InternalNote)
	}
	if len(h.wh.orders) != 1 {
		t.Fatalf("cut placed another warehouse order: %+v", h.wh.orders)
	}
	e := m.emails()
	last := e[len(e)-1]
	mustContain(t, "changed", last.Body, "request #", "Sti/A: you asked for 5, we're sending 1", "A note from Hack Club:", "Our pins ran out.", "- 1 × Sti/A", "- 2 × Sti/B", "/requests")
	mustContain(t, "changed subject", last.Subject, "changed")
	if !strings.HasSuffix(last.IdempotencyKey, "-changed") {
		t.Errorf("idempotency key = %q", last.IdempotencyKey)
	}
	if ev := h.events("request_changed"); len(ev) != 1 {
		t.Fatalf("request_changed = %+v", ev)
	} else if p := props(t, ev[0]); p["lowered"] != float64(1) || p["has_note"] != true || p["items_shipped"] != float64(3) || p["admin_id"] == nil {
		t.Fatalf("request_changed props = %v", p)
	} else if strings.Contains(string(ev[0].Props), confirmer) {
		t.Fatalf("request_changed logged who confirmed: %s", ev[0].Props)
	} else if ev[0].UserID == nil || *ev[0].UserID != r.UserID {
		t.Fatalf("request_changed belongs to %v, want the requester", ev[0].UserID)
	}

	// The review agent can do the same; a line cut to none says so plainly.
	k := h.mkKey("Review agent")
	other := mk("other@x.com", 3)
	// Approved with a cut the shipped email already explained.
	h.do(admin, "POST", fmt.Sprintf("/api/admin/requests/%d/dispatch", other.ID), map[string]any{"lines": []map[string]any{{"sku": "Sti/B", "quantity": 1}}}, nil)
	if code := h.do(h.keyClient(k.Token), "POST", fmt.Sprintf("/api/agent/requests/%d/lines", other.ID),
		map[string]any{"lines": []map[string]any{{"sku": "Sti/A", "quantity": 0}}, "warehouse_confirmed_by": confirmer}, &got); code != 200 {
		t.Fatalf("agent cut = %d", code)
	}
	e = m.emails()
	last = e[len(e)-1]
	mustContain(t, "cut to none", last.Body, "Sti/A: you asked for 3, we're not sending any this time")
	if strings.Contains(last.Body, "× Sti/A") || strings.Contains(last.Body, "A note from Hack Club") || strings.Contains(last.Body, "Sti/B: you asked") {
		t.Errorf("cut-to-none email:\n%s", last.Body)
	}
	if ev := h.events("request_changed"); len(ev) != 2 || props(t, ev[1])["api_key_id"] == nil {
		t.Fatalf("agent request_changed = %+v", ev)
	}

	// An earlier cut can be undone, up to what was asked for, with its own email.
	if code := h.do(admin, "POST", path, map[string]any{"lines": []map[string]any{{"sku": "Sti/A", "quantity": 6}}, "warehouse_confirmed_by": confirmer}, nil); code != 422 {
		t.Fatalf("raise past the ask = %d, want 422", code)
	}
	if code := h.do(admin, "POST", path, map[string]any{"lines": []map[string]any{{"sku": "Sti/A", "quantity": 5}}, "note": "We found more pins.", "warehouse_confirmed_by": confirmer}, &got); code != 200 {
		t.Fatalf("restore = %d", code)
	}
	for _, l := range got.Lines {
		if l.SKU == "Sti/A" && (l.Quantity != 5 || *l.RequestedQuantity != 5) {
			t.Fatalf("restored line = %+v", l)
		}
	}
	e = m.emails()
	last = e[len(e)-1]
	mustContain(t, "restored", last.Body, "Sti/A: we're sending 5 after all, not 1", "We found more pins.", "- 5 × Sti/A")
	if strings.Contains(last.Body, "Our pins ran out.") || !strings.HasSuffix(last.IdempotencyKey, "-changed-2") {
		t.Errorf("restore email (key %q):\n%s", last.IdempotencyKey, last.Body)
	}
	if ev := h.events("request_changed"); len(ev) != 3 || props(t, ev[2])["raised"] != float64(1) || props(t, ev[2])["lowered"] != float64(0) {
		t.Fatalf("restore request_changed = %+v", ev)
	}

	// Once the warehouse has mailed it, it's too late.
	now := time.Now()
	if _, err := h.store.SetWarehouseCosts(context.Background(), r.ID, theseus.Costs{}, &now); err != nil {
		t.Fatal(err)
	}
	if code := h.do(admin, "POST", path, map[string]any{"lines": []map[string]any{{"sku": "Sti/B", "quantity": 1}}, "warehouse_confirmed_by": confirmer}, nil); code != 409 {
		t.Fatalf("mailed request = %d, want 409", code)
	}
}

// A card-paid request's postage was priced on what it ships, so cutting it
// would owe a refund this action doesn't make.
func TestReduceRefusesCardPaid(t *testing.T) {
	h, fs, _ := newRefundHarness(t)
	r := paidIntlRequest(t, h, fs, userEmail)
	admin := h.client(adminEmail)
	if code := h.do(admin, "POST", fmt.Sprintf("/api/admin/requests/%d/dispatch", r.ID), map[string]any{}, nil); code != 200 {
		t.Fatalf("dispatch = %d", code)
	}
	if code := h.do(admin, "POST", fmt.Sprintf("/api/admin/requests/%d/lines", r.ID),
		map[string]any{"lines": []map[string]any{{"sku": r.Lines[0].SKU, "quantity": 0}}, "warehouse_confirmed_by": confirmer}, nil); code != 409 {
		t.Fatalf("card-paid cut = %d, want 409", code)
	}
}
