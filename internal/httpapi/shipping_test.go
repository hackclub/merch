package httpapi

import (
	"context"
	"strings"
	"testing"

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
	poster, postcard, magazine := shippingFixture(t, h)
	var errBody map[string]string
	code := h.do(h.client(userEmail), "POST", "/api/shipping/estimate", map[string]any{"country": "US",
		"lines": []swag.Line{{ItemID: poster.ID, Quantity: 1}, {ItemID: postcard.ID, Quantity: 50}}}, &errBody)
	if code != 422 || !strings.Contains(errBody["error"], "StayFlat") {
		t.Fatalf("poster + 50 postcards = %d %v", code, errBody)
	}
	code = h.do(h.client(userEmail), "POST", "/api/shipping/estimate", map[string]any{"country": "DE",
		"lines": []swag.Line{{ItemID: magazine.ID, Quantity: 5}}}, &errBody)
	if code != 422 || !strings.Contains(errBody["error"], "3 lb") {
		t.Fatalf("5 magazines abroad = %d %v", code, errBody)
	}
}

func TestRequestsOver3lbAbroadAreRefused(t *testing.T) {
	h := newHarness(t)
	_, _, magazine := shippingFixture(t, h)
	user := h.client(userEmail)
	var errBody map[string]string
	code := h.do(user, "POST", "/api/requests", map[string]any{"purpose": "club", "note": "for our club", "address": deAddr,
		"lines": []swag.Line{{ItemID: magazine.ID, Quantity: 5}}}, &errBody)
	if code != 422 || !strings.Contains(errBody["error"], "3 lb") {
		t.Fatalf("5 magazines to Germany = %d %v", code, errBody)
	}
	var r store.Request
	if code := h.do(user, "POST", "/api/requests", map[string]any{"purpose": "club", "note": "for our club", "address": deAddr,
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
	if out.International.TransitDays[0] < 10 || out.US.ArriveBy != "" {
		t.Fatalf("got %+v", out)
	}
}

func TestInternationalFeeIsEstimatedPostage(t *testing.T) {
	h := newHarness(t)
	_, _, magazine := shippingFixture(t, h)
	var r struct {
		store.Request
		PaymentURL string `json:"payment_url"`
	}
	code := h.do(h.client(userEmail), "POST", "/api/requests", map[string]any{"purpose": "club", "note": "for our club", "address": deAddr,
		"lines": []swag.Line{{ItemID: magazine.ID, Quantity: 1}}}, &r)
	if code != 201 || r.Status != swag.StatusAwaitingPayment || r.ShippingFeeCents != 900 || !strings.Contains(r.PaymentURL, "amount=900") {
		t.Fatalf("create = %d %+v", code, r)
	}
}

func TestShippingEstimateCostsForAdmins(t *testing.T) {
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
	if code := h.do(h.client(userEmail), "POST", "/api/shipping/estimate", body, &out); code != 200 || out.Costs != nil {
		t.Fatalf("requesters don't see costs: %d %+v", code, out.Costs)
	}
	if code := h.do(h.client(adminEmail), "POST", "/api/shipping/estimate", body, &out); code != 200 || out.Costs == nil {
		t.Fatalf("admin = %d %+v", code, out.Costs)
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
