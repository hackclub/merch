package main

import (
	"testing"
	"time"

	"github.com/hackclub/merch/internal/importer"
)

func TestApplyCache(t *testing.T) {
	day := func(s string) time.Time { d, _ := time.Parse(time.DateOnly, s); return d }
	ship := func(tracking, shipped string) importer.Shipment {
		return importer.Shipment{Tracking: tracking, ShippedOn: day(shipped)}
	}
	ships := []importer.Shipment{
		ship("AS1", "2026-09-01"),     // cached Asendia
		ship("AS2", "2026-09-02"),     // Asendia to look up
		ship("9400111", "2026-09-03"), // USPS, delivery date found in Safari
		ship("1Z222", "2026-09-04"),   // UPS, not looked up: never sent to Asendia
		ship("AS3", "2026-01-01"),     // before -since
		ship("", "2026-09-05"),        // letter, no tracking
	}
	cache := map[string]time.Time{"AS1": day("2026-09-12"), "9400111": day("2026-09-06")}
	todo := applyCache(ships, cache, "2026-06-01")
	if len(todo) != 1 || ships[todo[0]].Tracking != "AS2" {
		t.Fatalf("todo = %v", todo)
	}
	if d := ships[0].DeliveredOn; d == nil || !d.Equal(day("2026-09-12")) {
		t.Fatalf("AS1 delivered = %v", d)
	}
	if d := ships[2].DeliveredOn; d == nil || !d.Equal(day("2026-09-06")) {
		t.Fatalf("USPS delivered = %v", d)
	}
	if ships[3].DeliveredOn != nil || ships[5].DeliveredOn != nil {
		t.Fatalf("got %+v", ships)
	}
}
