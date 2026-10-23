package swag

import (
	"errors"
	"reflect"
	"testing"
)

func TestCustomsInvoice(t *testing.T) {
	// A Magazine 2025 (which ships with its intro card) and a shirt.
	contents := []Shipped{
		{ItemID: 1, SKU: "Pri/Bok/M25/1st", Quantity: 2},
		{ItemID: 1, SKU: "Pri/M25/4x6/1st", Quantity: 2},
		{ItemID: 2, SKU: "Swa/Shirt/HC/M", Quantity: 1},
	}
	names := map[string]string{"Pri/Bok/M25/1st": "Magazine 2025", "Swa/Shirt/HC/M": "Hack Club T-shirt (M)"}
	skus := map[string]CustomsSKU{
		"Pri/Bok/M25/1st": {Name: "Magazine", UnitCost: 4.1027},
		"Pri/M25/4x6/1st": {Name: "Magazine intro card", UnitCost: 0.17473},
		"Swa/Shirt/HC/M":  {Name: "Shirt M", UnitCost: 13.8166, Description: "Cotton T-shirt", HSCode: "6109.10", Origin: "US"},
	}
	inv, err := CustomsInvoice(contents, names, skus)
	if err != nil {
		t.Fatal(err)
	}
	want := []InvoiceLine{
		// Our name for what they asked for, the warehouse's for what ships with it,
		// and the warehouse's customs description over both when it has one.
		{SKU: "Pri/Bok/M25/1st", Description: "Magazine 2025", Quantity: 2, UnitValue: 4.1027, ValueCents: 821},
		{SKU: "Pri/M25/4x6/1st", Description: "Magazine intro card", Quantity: 2, UnitValue: 0.17473, ValueCents: 35},
		{SKU: "Swa/Shirt/HC/M", Description: "Cotton T-shirt", HSCode: "6109.10", Origin: "US", Quantity: 1, UnitValue: 13.8166, ValueCents: 1382},
	}
	if !reflect.DeepEqual(inv.Lines, want) {
		t.Fatalf("lines =\n%+v\nwant\n%+v", inv.Lines, want)
	}
	if inv.TotalCents != 821+35+1382 {
		t.Fatalf("total = %d", inv.TotalCents)
	}
}

// The values are what the warehouse declares with the parcel, so a SKU it has
// no value for can't go on the invoice.
func TestCustomsInvoiceNeedsEveryValue(t *testing.T) {
	contents := []Shipped{{ItemID: 1, SKU: "Sti/A", Quantity: 3}, {ItemID: 2, SKU: "Sti/B", Quantity: 1}}
	for name, skus := range map[string]map[string]CustomsSKU{
		"missing": {"Sti/A": {UnitCost: 0.25}},
		"zero":    {"Sti/A": {UnitCost: 0.25}, "Sti/B": {}},
	} {
		if _, err := CustomsInvoice(contents, nil, skus); !errors.Is(err, ErrNoCustomsValue) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}
