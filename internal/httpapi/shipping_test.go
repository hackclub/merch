package httpapi

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/hackclub/merch/internal/importer"
	"github.com/hackclub/merch/internal/store"
	"github.com/hackclub/merch/internal/swag"
)

func fp(v float64) *float64 { return &v }

// shippingFixture lists a poster, postcards and a magazine, and 12 past
// parcels to Germany that took 10-14 days.
func shippingFixture(t *testing.T, h *harness) (poster, postcard, magazine swag.Item) {
	t.Helper()
	ctx := context.Background()
	mk := func(it swag.Item) swag.Item {
		it.Visible, it.MaxPerRequest = true, 150
		saved, err := h.store.CreateItem(ctx, it)
		if err != nil {
			t.Fatal(err)
		}
		return saved
	}
	poster = mk(swag.Item{SKU: "Pri/HPd/11x/1st", Name: "Poster", WidthIn: fp(11), HeightIn: fp(17), ThicknessIn: fp(0.008)})
	postcard = mk(swag.Item{SKU: "Pri/Wel/4x6/1st", Name: "Postcard", WidthIn: fp(6), HeightIn: fp(4), ThicknessIn: fp(0.016)})
	magazine = mk(swag.Item{SKU: "Pri/Bok/M25/1st", Name: "Magazine", WidthIn: fp(5.83), HeightIn: fp(8.27), ThicknessIn: fp(0.3), WeightOz: fp(10.8)})
	var csv strings.Builder
	for i := range 12 {
		csv.WriteString("zenventory,DE,parcel,13,900,2026-09-01,2026-09-02,2026-09-" + []string{"12", "13", "14", "15", "16"}[i%5] + ",\n")
	}
	ships, err := importer.ParseShipments(strings.NewReader(csv.String()))
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.ReplaceShippingHistory(ctx, ships); err != nil {
		t.Fatal(err)
	}
	return
}

func TestShippingEstimate(t *testing.T) {
	h := newHarness(t)
	_, _, magazine := shippingFixture(t, h)
	body := map[string]any{"country": "de", "lines": []swag.Line{{ItemID: magazine.ID, Quantity: 1}}}

	var out struct {
		Estimate swag.Estimate
		Packing  swag.Packing
	}
	if code := h.do(h.client(userEmail), "POST", "/api/shipping/estimate", body, &out); code != 200 {
		t.Fatalf("estimate = %d", code)
	}
	e := out.Estimate
	if e.Service != "Asendia" || e.TransitBasis != "DE" || e.ArriveBy == "" || out.Packing.Package.Name != "Bubble mailer" {
		t.Fatalf("got %+v / %+v", e, out.Packing)
	}
	if out.Packing.LimitOz != 48 || out.Packing.WeightOz <= 0 {
		t.Errorf("abroad, the quote gives the 3 lb limit: %+v", out.Packing)
	}
	if e.PostageCents != [2]int{} || e.PostageTypicalCents != 0 {
		t.Fatalf("postage is for admins only, got %v", e.PostageCents)
	}
	// Abroad, the requester pays the typical postage for this package.
	if e.FeeCents != 900 {
		t.Fatalf("fee = %d, want 900", e.FeeCents)
	}
	if h.do(h.client(adminEmail), "POST", "/api/shipping/estimate", body, &out); out.Estimate.PostageCents[0] != 900 {
		t.Fatalf("admin postage = %v", out.Estimate.PostageCents)
	}
}

func TestShippingEstimateRefusesWhatWontShip(t *testing.T) {
	h := newHarness(t)
	poster, _, magazine := shippingFixture(t, h)
	var errBody map[string]string
	code := h.do(h.client(userEmail), "POST", "/api/shipping/estimate", map[string]any{"country": "US",
		"lines": []swag.Line{{ItemID: poster.ID, Quantity: 1}, {ItemID: magazine.ID, Quantity: 150}}}, &errBody)
	if code != 422 || !strings.Contains(errBody["error"], "StayFlat") {
		t.Fatalf("poster + 150 magazines = %d %v", code, errBody)
	}
	var heavy struct{ Error string }
	code = h.do(h.client(userEmail), "POST", "/api/shipping/estimate", map[string]any{"country": "DE",
		"lines": []swag.Line{{ItemID: magazine.ID, Quantity: 5}}}, &heavy)
	if code != 422 || !strings.Contains(heavy.Error, "3 lb") {
		t.Fatalf("5 magazines abroad = %d %v", code, heavy)
	}
}

// Over 3 lb abroad, the 422 gives the numbers, so the shop shows how far over
// it is and what weighs the most without packing it again.
func TestShippingEstimateSaysHowHeavy(t *testing.T) {
	h := newHarness(t)
	poster, _, magazine := shippingFixture(t, h)
	var heavy struct {
		Error    string
		WeightOz float64 `json:"weight_oz"`
		LimitOz  float64 `json:"limit_oz"`
		TareOz   float64 `json:"tare_oz"`
		Package  string
		Lines    []swag.LineWeight
	}
	code := h.do(h.client(userEmail), "POST", "/api/shipping/estimate", map[string]any{"country": "DE",
		"lines": []swag.Line{{ItemID: poster.ID, Quantity: 1}, {ItemID: magazine.ID, Quantity: 5}}}, &heavy)
	if code != 422 || !strings.HasPrefix(heavy.Error, swag.ErrTooHeavy.Error()) {
		t.Fatalf("too heavy = %d %+v", code, heavy)
	}
	if heavy.LimitOz != 48 || heavy.WeightOz <= 48 || heavy.TareOz <= 0 || heavy.Package == "" {
		t.Errorf("weights = %+v", heavy)
	}
	if len(heavy.Lines) != 2 || heavy.Lines[0].ItemID != magazine.ID || heavy.Lines[0].Name != "Magazine" ||
		heavy.Lines[0].Quantity != 5 || heavy.Lines[0].Oz != 54 || heavy.Lines[1].ItemID != poster.ID {
		t.Errorf("lines = %+v", heavy.Lines)
	}
	// In the US there's no limit to show.
	var out struct{ Packing map[string]any }
	h.do(h.client(userEmail), "POST", "/api/shipping/estimate", map[string]any{"country": "US",
		"lines": []swag.Line{{ItemID: magazine.ID, Quantity: 1}}}, &out)
	if _, ok := out.Packing["limit_oz"]; ok {
		t.Errorf("US packing = %v", out.Packing)
	}
}

func TestRequestsOver3lbAbroadAreRefused(t *testing.T) {
	h := newHarness(t)
	_, _, magazine := shippingFixture(t, h)
	user := h.client(userEmail)
	var errBody map[string]string
	code := h.do(user, "POST", "/api/requests", map[string]any{"purpose": "club", "note": "for our club", "customs_ok": true, "address": deAddr,
		"lines": []swag.Line{{ItemID: magazine.ID, Quantity: 5}}}, &errBody)
	if code != 422 || !strings.Contains(errBody["error"], "3 lb") {
		t.Fatalf("5 magazines to Germany = %d %v", code, errBody)
	}
	var r store.Request
	if code := h.do(user, "POST", "/api/requests", map[string]any{"purpose": "club", "note": "for our club", "customs_ok": true, "address": deAddr,
		"lines": []swag.Line{{ItemID: magazine.ID, Quantity: 2}}}, &r); code != 201 {
		t.Fatalf("2 magazines to Germany = %d", code)
	}
	// While it waits to ship, the requester sees when it should arrive.
	var mine struct {
		Requests []struct {
			ID       int64
			Estimate *swag.Estimate
		}
	}
	h.do(user, "GET", "/api/requests", nil, &mine)
	if len(mine.Requests) != 1 || mine.Requests[0].Estimate == nil || mine.Requests[0].Estimate.ArriveBy == "" {
		t.Fatalf("got %+v", mine.Requests)
	}
}

func TestShippingSummary(t *testing.T) {
	h := newHarness(t)
	shippingFixture(t, h)
	var out struct{ US, International swag.Estimate }
	if code := h.do(h.client(""), "GET", "/api/shipping", nil, &out); code != 200 {
		t.Fatalf("summary = %d", code)
	}
	// Wed 09-02 to Sat 09-12 at the quickest: 7 business days.
	if out.International.TransitDays[0] != 7 || out.US.ArriveBy != "" {
		t.Fatalf("got %+v", out)
	}
}

func TestInternationalFeeIsEstimatedPostage(t *testing.T) {
	h := newHarness(t)
	_, _, magazine := shippingFixture(t, h)
	var r store.Request
	code := h.do(h.client(userEmail), "POST", "/api/requests", map[string]any{"purpose": "club", "note": "for our club", "customs_ok": true, "address": deAddr,
		"lines": []swag.Line{{ItemID: magazine.ID, Quantity: 1}}}, &r)
	if code != 201 || r.Status != swag.StatusAwaitingPayment || r.ShippingFeeCents != 900 {
		t.Fatalf("create = %d %+v", code, r)
	}
}

func TestShippingEstimateCostsForEveryone(t *testing.T) {
	h := newHarness(t)
	_, _, magazine := shippingFixture(t, h)
	sticker, err := h.store.CreateItem(context.Background(), swag.Item{SKU: "Sti/A", Name: "Sticker A", Packs: "sticker", Visible: true, MaxPerRequest: 50})
	if err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"country": "US", "lines": []swag.Line{{ItemID: magazine.ID, Quantity: 1}, {ItemID: sticker.ID, Quantity: 2}}}
	var out struct {
		Costs *struct{ US, International *swag.Costs }
	}
	// Hack Club is financially transparent: requesters see what their request costs it, too.
	if code := h.do(h.client(userEmail), "POST", "/api/shipping/estimate", body, &out); code != 200 || out.Costs == nil {
		t.Fatalf("requester = %d %+v", code, out.Costs)
	}
	us, intl := out.Costs.US, out.Costs.International
	// Stickers at $0.25 from the warehouse; it has no cost for the magazine.
	// AGH: $1.80 + $0.20 for each of the 2 SKUs, and a bubble mailer.
	if us.ItemsCents != 50 || len(us.UnpricedSKUs) != 1 || us.LaborCents != 220 || us.PackingCents != 50 {
		t.Fatalf("us = %+v", us)
	}
	// Postage comes from history: none in the US, $9 parcels abroad.
	if us.PostageCents != 0 || intl.PostageCents != 900 || intl.TotalCents != 50+220+50+900 {
		t.Fatalf("us = %+v, intl = %+v", us, intl)
	}
}

// A new pod loads shipping history before anyone asks, so the first visitor
// after a deploy doesn't wait on 47k rows over a tenth of a CPU.
func TestWarmLoadsShippingHistory(t *testing.T) {
	h := newHarness(t)
	shippingFixture(t, h)
	s := &server{Deps: Deps{Config: h.cfg, Store: h.store, Now: time.Now, Log: slog.Default()}}
	s.warm(context.Background())
	warm := s.history.h
	if warm == nil {
		t.Fatal("warm didn't load shipping history")
	}
	parcel := swag.Packing{Package: swag.Package{Class: swag.ClassParcel}, ContentsOz: 13}
	if e := warm.Estimate("DE", parcel, time.Now()); e.TransitSamples != 12 {
		t.Fatalf("warm history has %d German parcels, want 12", e.TransitSamples)
	}
	if got, err := s.shippingHistory(context.Background()); err != nil || got != warm {
		t.Fatalf("estimates reloaded the history instead of using the warm copy (err %v)", err)
	}
}

// Admins reviewing a request see what it costs Hack Club to send: items at the
// warehouse's unit costs, AGH's labor, the package and typical postage.
func TestAdminRequestListShowsCosts(t *testing.T) {
	h := newHarness(t)
	it := h.mkItem("Sti/A", true)
	user := h.client(userEmail)
	if code := h.do(user, "POST", "/api/requests", map[string]any{"purpose": "personal", "address": usAddr, "lines": []swag.Line{{ItemID: it.ID, Quantity: 3}}}, nil); code != 201 {
		t.Fatalf("create = %d", code)
	}
	var all struct{ Requests []struct{ Costs *swag.Costs } }
	h.do(h.client(adminEmail), "GET", "/api/admin/requests", nil, &all)
	c := all.Requests[0].Costs
	if c == nil {
		t.Fatal("admin list has no costs")
	}
	if c.ItemsCents != 75 || c.LaborCents != swag.OrderLaborCents+swag.SKULaborCents ||
		c.TotalCents != c.ItemsCents+c.LaborCents+c.PackingCents+c.PostageCents {
		t.Errorf("costs = %+v", c)
	}
	var mine struct{ Requests []map[string]any }
	h.do(user, "GET", "/api/requests", nil, &mine)
	if _, ok := mine.Requests[0]["costs"]; ok {
		t.Errorf("requester sees costs: %v", mine.Requests[0])
	}
}

// A backordered item ships once its restock reaches the warehouse: the
// catalog says what's inbound, and the estimate counts from the restock.
func TestBackorderedItemsWaitForRestock(t *testing.T) {
	h := newHarness(t)
	_, postcard, _ := shippingFixture(t, h)
	haven, err := h.store.CreateItem(context.Background(), swag.Item{SKU: "Pri/Hav/4x6/1st", Name: "Haven Postcard",
		Visible: true, MaxPerRequest: 150, WidthIn: fp(6), HeightIn: fp(4), ThicknessIn: fp(0.016)})
	if err != nil {
		t.Fatal(err)
	}

	var cat struct {
		Stock map[string]struct {
			InStock int `json:"in_stock"`
			Inbound int `json:"inbound"`
		} `json:"stock"`
	}
	if code := h.do(h.client(""), "GET", "/api/catalog", nil, &cat); code != 200 {
		t.Fatalf("catalog = %d", code)
	}
	if st := cat.Stock["Pri/Hav/4x6/1st"]; st.InStock != -503 || st.Inbound != 15000 {
		t.Fatalf("haven stock = %+v (all %+v)", st, cat.Stock)
	}
	if _, ok := cat.Stock["Pri/Wel/4x6/1st"]; !ok {
		t.Fatalf("every listed sku has stock (out of stock when the warehouse omits it): %+v", cat.Stock)
	}

	estimate := func(lines ...swag.Line) swag.Estimate {
		t.Helper()
		var out struct{ Estimate swag.Estimate }
		if code := h.do(h.client(userEmail), "POST", "/api/shipping/estimate", map[string]any{"country": "DE", "lines": lines}, &out); code != 200 {
			t.Fatalf("estimate = %d", code)
		}
		return out.Estimate
	}
	if e := estimate(swag.Line{ItemID: postcard.ID, Quantity: 25}); e.Restock || e.ArriveBy == "" {
		t.Fatalf("in stock: %+v", e)
	}
	e := estimate(swag.Line{ItemID: postcard.ID, Quantity: 25}, swag.Line{ItemID: haven.ID, Quantity: 25})
	if !e.Restock || e.ArriveBy != "" || e.TransitDays == [2]int{} {
		t.Fatalf("backordered: %+v", e)
	}
	evs := h.events("shipping_estimate")
	if p := props(t, evs[len(evs)-1]); p["restock"] != true {
		t.Fatalf("shipping_estimate props = %v", p)
	}
}
