package swag

import "math"

// AGH Fulfillment's labor rates, from its invoices (2024-2026): "Order
// Shipped" and "SKU Shipped", which counts each different SKU in an order
// once, however many of it go in.
const (
	OrderLaborCents = 180
	SKULaborCents   = 20
)

// Costs is what a request costs Hack Club to send. Postage is the typical
// (median) postage of past packages like it; 0 when there's no history.
type Costs struct {
	ItemsCents   int `json:"items_cents"`
	LaborCents   int `json:"labor_cents"`
	PackingCents int `json:"packing_cents"`
	PostageCents int `json:"postage_cents"`
	TotalCents   int `json:"total_cents"`
	// UnpricedSKUs have no warehouse unit cost, so ItemsCents leaves them out.
	UnpricedSKUs []string `json:"unpriced_skus"`
}

// CostToShip totals what a box costs Hack Club: its contents (Shipment) at
// the warehouse's unit costs in dollars, AGH's labor, the package and postage.
func CostToShip(contents []Shipped, unitCost map[string]float64, p Packing, e Estimate) Costs {
	c := Costs{UnpricedSKUs: []string{}}
	var dollars float64
	skus := map[string]bool{}
	for _, s := range contents {
		skus[s.SKU] = true
		if u, ok := unitCost[s.SKU]; ok {
			dollars += u * float64(s.Quantity)
		} else {
			c.UnpricedSKUs = append(c.UnpricedSKUs, s.SKU)
		}
	}
	c.ItemsCents = int(math.Round(dollars * 100))
	c.LaborCents = OrderLaborCents + SKULaborCents*len(skus)
	c.PackingCents = p.Package.CostCents
	c.PostageCents = e.PostageTypicalCents
	c.TotalCents = c.ItemsCents + c.LaborCents + c.PackingCents + c.PostageCents
	return c
}
