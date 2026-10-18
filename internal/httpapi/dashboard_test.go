package httpapi

import (
	"fmt"
	"testing"

	"github.com/hackclub/merch/internal/auth"
	"github.com/hackclub/merch/internal/store"
	"github.com/hackclub/merch/internal/swag"
)

type dashboardResponse struct {
	Summary swag.SpendSummary      `json:"summary"`
	Costs   map[string]shippedCost `json:"costs"`
	Queue   struct {
		Requests   int `json:"requests"`
		TotalCents int `json:"total_cents"`
	} `json:"queue"`
}

func TestAdminDashboard(t *testing.T) {
	h := newHarness(t)
	it := h.mkItem("Sti/A", true)
	user, admin := h.client(userEmail), h.client(adminEmail)
	lines := []swag.Line{{ItemID: it.ID, Quantity: 2}}

	var us, de, waiting store.Request
	h.do(user, "POST", "/api/requests", map[string]any{"purpose": "personal", "address": usAddr, "lines": lines}, &us)
	h.do(user, "POST", "/api/requests", map[string]any{"purpose": "personal", "customs_ok": true, "address": deAddr, "lines": lines}, &de)
	h.payByCard(userEmail, de.ID)
	h.do(user, "POST", "/api/requests", map[string]any{"purpose": "personal", "address": usAddr, "lines": lines}, &waiting)
	for _, id := range []int64{us.ID, de.ID} {
		if code := h.do(admin, "POST", fmt.Sprintf("/api/admin/requests/%d/dispatch", id), map[string]any{}, nil); code != 200 {
			t.Fatalf("dispatch %d = %d", id, code)
		}
	}

	if code := h.do(user, "GET", "/api/admin/dashboard", nil, nil); code != 403 {
		t.Fatalf("user = %d", code)
	}
	var d dashboardResponse
	if code := h.do(admin, "GET", "/api/admin/dashboard?days=7", nil, &d); code != 200 {
		t.Fatalf("dashboard = %d", code)
	}
	if len(d.Summary.Days) != 7 || d.Summary.Days[6].Requests != 2 || d.Summary.Totals.Requests != 2 || d.Summary.Month.Requests != 2 {
		t.Fatalf("summary = %+v", d.Summary)
	}
	// The warehouse said what contents and labor cost when it took the order; postage is still an estimate.
	c := d.Costs[fmt.Sprint(us.ID)]
	if c.ItemsCents != 50 || c.LaborCents != 220 || fmt.Sprint(c.Actual) != "[items labor]" {
		t.Fatalf("US costs = %+v", c)
	}
	if d.Queue.Requests != 1 || d.Queue.TotalCents == 0 {
		t.Fatalf("queue = %+v", d.Queue)
	}
	in := d.Summary.International
	if in.Requests != 1 || in.CollectedCents != 1500 || in.PostageActual != 0 || len(d.Summary.ByCountry) != 1 || d.Summary.ByCountry[0].Country != "DE" {
		t.Fatalf("international = %+v %+v", in, d.Summary.ByCountry)
	}

	// Pulling from the warehouse brings in each label's postage and tracking.
	var pulled struct{ Checked, Updated, Failed int }
	if code := h.do(admin, "POST", "/api/admin/requests/refresh", map[string]any{}, &pulled); code != 200 || pulled.Checked != 2 || pulled.Updated != 2 {
		t.Fatalf("refresh = %d %+v", code, pulled)
	}
	evs := h.events("requests_refreshed")
	if len(evs) != 1 || props(t, evs[0])["updated"] != float64(2) {
		t.Fatalf("events = %+v", evs)
	}
	h.do(admin, "GET", "/api/admin/dashboard?days=7", nil, &d)
	if c := d.Costs[fmt.Sprint(de.ID)]; c.PostageCents != 471 || fmt.Sprint(c.Actual) != "[items labor postage]" {
		t.Fatalf("DE costs = %+v", c)
	}
	if in := d.Summary.International; in.PostageCents != 471 || in.PostageActual != 1 || in.NetCents != 1500-471 {
		t.Fatalf("international = %+v", in)
	}
	got, _ := h.store.GetRequest(t.Context(), us.ID)
	if got.TrackingNumber != "9400" || got.MailedAt == nil {
		t.Fatalf("us = %+v", got)
	}
	// Requests that have their postage aren't asked about again.
	before := h.wh.gets
	h.do(admin, "POST", "/api/admin/requests/refresh", map[string]any{}, &pulled)
	if pulled.Checked != 0 || h.wh.gets != before {
		t.Fatalf("second refresh = %+v, %d gets", pulled, h.wh.gets-before)
	}
}

// Imports shipped abroad over the 3 lb limit (50 magazines) are still priced.
func TestDashboardPricesOverweightImports(t *testing.T) {
	h := newHarness(t)
	heavy := 20.0
	it, err := h.store.CreateItem(t.Context(), swag.Item{SKU: "Sti/A", Name: "Heavy", Visible: true, MaxPerRequest: 10, WeightOz: &heavy})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := h.store.UpsertUser(t.Context(), auth.Identity{ID: "ident!u", Email: userEmail})
	r, err := h.store.CreateRequest(t.Context(), store.NewRequest{UserID: u.ID, Address: deAddr, Status: swag.StatusPending,
		Lines: []swag.Line{{ItemID: it.ID, Quantity: 5}}}, func(map[int64]int) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	h.store.MarkDispatched(t.Context(), r.ID, "pkg!old", store.ShipChanges{})
	var d dashboardResponse
	h.do(h.client(adminEmail), "GET", "/api/admin/dashboard?days=7", nil, &d)
	if c := d.Costs[fmt.Sprint(r.ID)]; c.PackingCents == 0 || c.ItemsCents != 125 {
		t.Fatalf("costs = %+v", c)
	}
}
