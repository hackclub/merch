package httpapi

import (
	"fmt"
	"github.com/hackclub/merch/internal/store"
	"testing"
)

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
