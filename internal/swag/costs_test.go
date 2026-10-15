package swag

import (
	"slices"
	"testing"
)

func TestCostToShip(t *testing.T) {
	bubble := Packing{Package: Package{Name: "Bubble mailer", CostCents: 50}}
	contents := []Shipped{
		{SKU: "Pri/Bok/M25/1st", Quantity: 2},
		{SKU: "Pri/M25/4x6/1st", Quantity: 2}, // ships with the magazine
		{SKU: "Sti/Bra/O&H/Lap", Quantity: 50},
		{SKU: "Sti/New/1st", Quantity: 3}, // the warehouse has no cost for it
	}
	unit := map[string]float64{"Pri/Bok/M25/1st": 1.64, "Pri/M25/4x6/1st": 0.0832, "Sti/Bra/O&H/Lap": 0.12}
	got := CostToShip(contents, unit, bubble, Estimate{PostageTypicalCents: 512})
	// 2×1.64 + 2×0.0832 + 50×0.12 = 9.4464
	if got.ItemsCents != 945 {
		t.Errorf("items = %d, want 945", got.ItemsCents)
	}
	// AGH: $1.80 an order and $0.20 for each SKU in it, plus the mailer.
	if got.LaborCents != 180+4*20 || got.PackingCents != 50 {
		t.Errorf("labor = %d, packing = %d", got.LaborCents, got.PackingCents)
	}
	if got.PostageCents != 512 || got.TotalCents != 945+260+50+512 {
		t.Errorf("postage = %d, total = %d", got.PostageCents, got.TotalCents)
	}
	if !slices.Equal(got.UnpricedSKUs, []string{"Sti/New/1st"}) {
		t.Errorf("unpriced = %v", got.UnpricedSKUs)
	}
}

func TestEveryPackageHasACost(t *testing.T) {
	for _, p := range Packages {
		// AGH doesn't bill for the envelopes stickers go out in.
		if p.CostCents <= 0 && p.Class != ClassLetter {
			t.Errorf("%s has no cost", p.Name)
		}
	}
}
