package store

import (
	"strings"
	"testing"
	"time"

	"github.com/hackclub/merch/internal/importer"
	"github.com/hackclub/merch/internal/swag"
)

func TestShippingHistory(t *testing.T) {
	s := newStore(t)
	parse := func(csv string) []importer.Shipment {
		t.Helper()
		ships, err := importer.ParseShipments(strings.NewReader(csv))
		if err != nil {
			t.Fatal(err)
		}
		return ships
	}
	if err := s.ReplaceShippingHistory(ctx, parse(`zenventory,GB,parcel,9.1,630,2026-09-11,2026-09-14,2026-09-26,AS1
zenventory,US,letter,,,2026-09-11,2026-09-12,,
theseus_letter,US,letter,1,74,,2026-08-03,2026-08-08,
`)); err != nil {
		t.Fatal(err)
	}
	// Reloading a source replaces its rows and leaves other sources alone.
	if err := s.ReplaceShippingHistory(ctx, parse(`zenventory,GB,parcel,9.1,630,2026-09-11,2026-09-14,2026-09-26,AS1
zenventory,CA,parcel,12,1010,2026-09-12,2026-09-14,,
`)); err != nil {
		t.Fatal(err)
	}
	h, err := s.ShippingHistory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(h) != 3 {
		t.Fatalf("got %d rows: %+v", len(h), h)
	}
	byCountry := map[string]swag.PastShipment{}
	for _, p := range h {
		byCountry[p.Country] = p
	}
	gb := byCountry["GB"]
	// Days are business days: Fri 09-11 -> Mon 09-14 -> Sat 09-26.
	if gb.Class != swag.ClassParcel || gb.WeightOz != 9.1 || gb.PostageCents != 630 || *gb.HandlingDays != 1 || *gb.TransitDays != 9 ||
		gb.ShippedOn.Format(time.DateOnly) != "2026-09-14" {
		t.Fatalf("GB = %+v", gb)
	}
	if ca := byCountry["CA"]; ca.TransitDays != nil {
		t.Fatalf("CA has no delivery date: %+v", ca)
	}
	if us := byCountry["US"]; us.HandlingDays != nil || *us.TransitDays != 4 {
		t.Fatalf("US letter = %+v", us)
	}
}

func TestItemWeight(t *testing.T) {
	s := newStore(t)
	w := 10.8
	it, err := s.CreateItem(ctx, swag.Item{SKU: "Pri/Bok/M25/1st", Name: "Magazine", MaxPerRequest: 5, WeightOz: &w})
	if err != nil {
		t.Fatal(err)
	}
	if it.WeightOz == nil || *it.WeightOz != 10.8 {
		t.Fatalf("weight = %v", it.WeightOz)
	}
	it.WeightOz = nil
	if it, err = s.UpdateItem(ctx, it); err != nil || it.WeightOz != nil {
		t.Fatalf("cleared weight = %v, %v", it.WeightOz, err)
	}
}
