package httpapi

import (
	"context"
	"fmt"
	"github.com/hackclub/merch/internal/store"
	"github.com/hackclub/merch/internal/swag"
	"strings"
	"testing"
	"time"
)

type fakePrintPO struct {
	p    swag.PrintPOEvidence
	fail bool
}

func (f *fakePrintPO) Exact(_ context.Context, l swag.PrintPOLink) (swag.PrintPOEvidence, error) {
	if f.fail || l != f.p.PrintPOLink {
		return swag.PrintPOEvidence{}, fmt.Errorf("missing exact line")
	}
	return f.p, nil
}
func (f *fakePrintPO) Suggest(context.Context, string) ([]swag.PrintPOEvidence, error) {
	return []swag.PrintPOEvidence{f.p}, nil
}

func TestPrintPOLinkReceiptAndReleaseAPI(t *testing.T) {
	now := time.Now()
	po := &fakePrintPO{}
	h := newHarness(t, func(d *Deps) {
		d.PrintPOs = po
		d.Config.PrintPOAutomatic = true
		d.Now = func() time.Time { return now }
	})
	owner, admin := h.client("owner@x.com"), h.client(adminEmail)
	var created store.PrintRequest
	body := map[string]any{"product": "poster_11x17", "finish": "gloss", "title": "Demo poster", "program": "Demo", "deadline": "Next month", "note": "Library displays", "quantity": 100, "front_url": "https://example.com/front.png", "front_preview_url": "https://example.com/front.jpg", "initial_quantity": 5, "address": deAddr}
	if c := h.do(owner, "POST", "/api/print-requests", body, &created); c != 201 {
		t.Fatal(c)
	}
	runs, _ := h.store.PrintRuns(context.Background(), 0)
	p := runs[0]
	path := fmt.Sprintf("/api/print-runs/%d/actions", p.ID)
	po.p = swag.PrintPOEvidence{PrintPOLink: swag.PrintPOLink{POID: 42, LineID: 10, SKU: "Pri/A"}}
	if c := h.do(admin, "POST", path, map[string]any{"action": "link", "version": p.Version, "sku": "Pri/A", "confirmation_url": "https://example.com/confirmation", "links": []swag.PrintPOLink{po.p.PrintPOLink}}, nil); c != 422 {
		t.Fatalf("link before approval: %d", c)
	}
	if items, err := h.store.ItemsBySKU(context.Background(), []string{"Pri/A"}); err != nil || len(items) != 0 {
		t.Fatalf("refused link created a listing: %+v %v", items, err)
	}
	if c := h.do(admin, "POST", path, map[string]any{"action": "approve", "version": p.Version, "approved_quantity": 100, "specifications": "Gloss; artwork and budget checked"}, &p); c != 200 {
		t.Fatal(c)
	}
	at := now.Add(time.Second)
	now = now.Add(2 * time.Second)
	po.p = swag.PrintPOEvidence{PrintPOLink: swag.PrintPOLink{POID: 42, LineID: 10, SKU: "Pri/A"}, Quantity: 100, CreatedAt: &at, SyncedAt: at, PolledAt: now}
	if c := h.do(admin, "POST", path, map[string]any{"action": "link", "version": p.Version, "sku": "Pri/A", "confirmation_url": "https://example.com/confirmation", "links": []swag.PrintPOLink{po.p.PrintPOLink}}, &p); c != 200 {
		t.Fatal(c)
	}
	p, _ = h.store.PrintRun(context.Background(), p.ID)
	if p.Stage != swag.PrintOrdered {
		t.Fatalf("stage %s", p.Stage)
	}
	its, _ := h.store.ItemsBySKU(context.Background(), []string{"Pri/A"})
	if len(its) != 1 || its[0].Visible || its[0].Finish != "gloss" {
		t.Fatalf("prepared %+v", its)
	}
	po.p.Completed = true
	po.p.CompletedAt = nil
	if c := h.do(admin, "POST", path, map[string]any{"action": "refresh_po", "version": p.Version}, &p); c != 200 || p.Stage != swag.PrintReceived {
		t.Fatalf("refresh %d %+v", c, p)
	}
	if c := h.do(admin, "POST", path, map[string]any{"action": "release", "version": p.Version}, &p); c != 200 || p.ShipmentRequestID == nil {
		t.Fatalf("release %d %+v", c, p)
	}
	parcel, _ := h.store.GetRequest(context.Background(), *p.ShipmentRequestID)
	if parcel.Status != swag.StatusAwaitingPayment || parcel.ShippingFeeCents <= 0 || parcel.PrintRunID == nil || *parcel.PrintRunID != p.ID {
		t.Fatalf("parcel %+v", parcel)
	}
	if len(h.wh.orders) != 0 {
		t.Fatal("release dispatched a warehouse order")
	}
	if len(h.events("print_po_polled")) < 2 {
		t.Fatal("polls were not instrumented")
	}
	before := len(h.events("print_po_polled"))
	if c := h.do(admin, "POST", path, map[string]any{"action": "refresh_po", "version": p.Version}, &p); c != 200 {
		t.Fatal(c)
	}
	if len(h.events("print_po_polled")) != before {
		t.Fatal("no-change poll must not emit usage activity")
	}
	version := p.Version
	po.fail = true
	for i := 0; i < 2; i++ {
		if c := h.do(admin, "POST", path, map[string]any{"action": "refresh_po", "version": version}, &p); c != 200 || p.Version != version || p.PollError == "" {
			t.Fatalf("failed poll metadata must not invalidate forms: %d %+v", c, p)
		}
	}
	po.fail = false
	if c := h.do(admin, "POST", path, map[string]any{"action": "refresh_po", "version": version}, &p); c != 200 || p.Version != version || p.PollError != "" {
		t.Fatalf("poll recovery changed form version: %d", c)
	}
	if len(h.events("print_po_polled")) != before+3 {
		t.Fatal("errors and recovery must be tracked without successful poll heartbeat noise")
	}
	// Retrying a completed release must return its parcel even if inventory is stale.
	h.wh.mu.Lock()
	h.wh.skuFail = fmt.Errorf("demo inventory source unavailable")
	h.wh.mu.Unlock()
	now = now.Add(2 * time.Minute)
	if c := h.do(admin, "POST", path, map[string]any{"action": "release", "version": 1}, &p); c != 200 || p.ShipmentRequestID == nil {
		t.Fatalf("idempotent stale-stock release %d", c)
	}
}

func TestRequesterPrintHistoryHidesPrivateOnlyUpdates(t *testing.T) {
	history := []store.PrintRunEvent{
		{ID: 5, Stage: swag.PrintApproved, Action: "update", Details: map[string]any{"eta": "2026-12-04", "eta_changed": true}},
		{ID: 4, Stage: swag.PrintApproved, Action: "update", Note: "New public update"},
		{ID: 3, Stage: swag.PrintApproved, Action: "update", Details: map[string]any{"specifications": "Same approved specification"}},
		{ID: 2, Stage: swag.PrintApproved, Action: "approve"},
		{ID: 1, Stage: swag.PrintSubmitted, Action: "submit"},
	}
	got := requesterPrintHistory(history)
	if len(got) != 4 || got[0].ID != 5 || got[1].ID != 4 || got[2].ID != 2 || got[3].ID != 1 {
		t.Fatalf("private-only update leaked into requester activity: %+v", got)
	}
}

// A size's SKU belongs to its sized item: linking it isn't a duplicate PO line.
func TestPrintLinkRefusesSizeSKU(t *testing.T) {
	po := &fakePrintPO{}
	h := newHarness(t, func(d *Deps) { d.PrintPOs = po })
	owner, admin := h.client("owner@x.com"), h.client(adminEmail)
	if _, err := h.store.CreateItem(context.Background(), swag.Item{SKU: "Swa/Hood", Name: "Hoodie", MaxPerRequest: 1,
		Variants: []swag.Variant{{Label: "S", SKU: "Swa/Hood/Small"}}}); err != nil {
		t.Fatal(err)
	}
	h.do(owner, "POST", "/api/print-requests", map[string]any{"product": "postcard_4x6", "title": "Demo cards", "program": "Demo", "deadline": "Next month", "note": "Welcome kits", "quantity": 1000, "front_url": "https://example.com/art.png", "initial_quantity": 25, "address": usAddr}, nil)
	runs, _ := h.store.PrintRuns(context.Background(), 0)
	p := runs[0]
	path := fmt.Sprintf("/api/print-runs/%d/actions", p.ID)
	if c := h.do(admin, "POST", path, map[string]any{"action": "approve", "version": p.Version, "approved_quantity": 1000, "specifications": "Linen; checked"}, &p); c != 200 {
		t.Fatal(c)
	}
	at := time.Now()
	po.p = swag.PrintPOEvidence{PrintPOLink: swag.PrintPOLink{POID: 7, LineID: 1, SKU: "Swa/Hood/Small"}, Quantity: 1000, CreatedAt: &at, SyncedAt: at, PolledAt: at}
	var got struct {
		Error string `json:"error"`
	}
	if c := h.do(admin, "POST", path, map[string]any{"action": "link", "version": p.Version, "sku": "Swa/Hood/Small", "confirmation_url": "https://example.com/confirmation", "links": []swag.PrintPOLink{po.p.PrintPOLink}}, &got); c != 422 || !strings.Contains(got.Error, "size of another item") {
		t.Fatalf("size SKU link = %d %q", c, got.Error)
	}
}

func TestPrintRunAccessAndActions(t *testing.T) {
	h := newHarness(t)
	admin := h.client(adminEmail)
	owner := h.client("owner@x.com")
	other := h.client("other@x.com")
	var p store.PrintRequest
	h.do(owner, "POST", "/api/print-requests", map[string]any{"product": "postcard_4x6", "title": "Demo cards", "program": "Demo", "deadline": "Next month", "note": "Welcome kits", "quantity": 1000, "front_url": "https://example.com/art.png", "initial_quantity": 25, "address": usAddr}, &p)
	var list struct {
		Runs []*store.PrintRun `json:"runs"`
	}
	if code := h.do(owner, "GET", "/api/print-runs", nil, &list); code != 200 || len(list.Runs) != 1 {
		t.Fatalf("list %d %+v", code, list)
	}
	r := list.Runs[0]
	path := fmt.Sprintf("/api/print-runs/%d", r.ID)
	if c := h.do(other, "GET", path, nil, nil); c != 404 {
		t.Fatalf("other detail %d", c)
	}
	if c := h.do(owner, "POST", path+"/actions", map[string]any{"action": "approve", "version": r.Version, "approved_quantity": 1000, "specifications": "Linen; art and budget checked"}, nil); c != 403 {
		t.Fatalf("owner approve %d", c)
	}
	if c := h.do(admin, "POST", path+"/actions", map[string]any{"action": "approve", "version": r.Version, "approved_quantity": 1000, "specifications": "Linen; art and budget checked"}, nil); c != 200 {
		t.Fatalf("approve %d", c)
	}
	if c := h.do(admin, "POST", path+"/actions", map[string]any{"action": "approve", "version": r.Version, "approved_quantity": 1000, "specifications": "Checked"}, nil); c != 409 {
		t.Fatalf("stale approve %d", c)
	}
	ev := h.events("print_run_action")
	if len(ev) != 1 || props(t, ev[0])["action"] != "approve" || props(t, ev[0])["specifications"] != nil {
		t.Fatalf("events %+v", ev)
	}
	if c := h.do(admin, "PUT", fmt.Sprintf("/api/admin/print-requests/%d", p.ID), map[string]any{"status": "stocked", "sku": "Demo/Card"}, nil); c != 409 {
		t.Fatalf("old unsafe update %d", c)
	}
	r, _ = h.store.PrintRun(context.Background(), r.ID)
	if c := h.do(owner, "POST", path+"/actions", map[string]any{"action": "cancel", "version": r.Version, "note": "Plans changed", "confirmation_url": "https://example.com/unverified-claim", "internal_note": "Should not be saved"}, nil); c != 200 {
		t.Fatalf("owner cancellation %d", c)
	}
	confirmations, _ := h.store.PrintConfirmations(context.Background(), r.ID)
	if len(confirmations) != 0 {
		t.Fatal("requester recorded a printer confirmation reserved for HQ")
	}
}
