package swag

import (
	"errors"
	"fmt"
	"math"
)

// ErrNoCustomsValue: the warehouse has no declared value for a SKU in the box.
var ErrNoCustomsValue = errors.New("no customs value")

// CustomsSKU is what mail.hackclub.com declares for a SKU when it ships one
// abroad: its value per unit in dollars (the SKU's declared unit cost) and,
// where the warehouse has them, a customs description, HS code and country of
// origin.
type CustomsSKU struct {
	Name        string
	Description string
	HSCode      string
	Origin      string
	UnitCost    float64
}

// InvoiceLine is one SKU on a customs invoice.
type InvoiceLine struct {
	SKU         string
	Description string
	HSCode      string
	Origin      string
	Quantity    int
	UnitValue   float64 // dollars, as precise as the warehouse declares it
	ValueCents  int
}

type Invoice struct {
	Lines      []InvoiceLine
	TotalCents int
}

// CustomsInvoice lists what's in the box (Shipment) at the values the
// warehouse declares with the parcel, so the invoice matches the customs data
// the carrier sends. names is our name for each requested SKU ("Hack Club
// T-shirt (M)"); ships-with SKUs use the warehouse's.
func CustomsInvoice(contents []Shipped, names map[string]string, skus map[string]CustomsSKU) (Invoice, error) {
	var inv Invoice
	for _, c := range contents {
		k, ok := skus[c.SKU]
		if !ok || k.UnitCost <= 0 {
			return Invoice{}, fmt.Errorf("%w for %s", ErrNoCustomsValue, c.SKU)
		}
		desc := k.Description
		if desc == "" {
			desc = names[c.SKU]
		}
		if desc == "" {
			desc = k.Name
		}
		l := InvoiceLine{SKU: c.SKU, Description: desc, HSCode: k.HSCode, Origin: k.Origin,
			Quantity: c.Quantity, UnitValue: k.UnitCost, ValueCents: int(math.Round(k.UnitCost * float64(c.Quantity) * 100))}
		inv.Lines = append(inv.Lines, l)
		inv.TotalCents += l.ValueCents
	}
	return inv, nil
}
