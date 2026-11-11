package httpapi

import (
	"context"
	"fmt"
	"github.com/hackclub/merch/internal/store"
	"github.com/hackclub/merch/internal/swag"
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
}
