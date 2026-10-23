package swag

import (
	"fmt"
	"math"
	"time"
)

// PrintProduct is something Hack Club staff can get printed through the shop:
// we order it from Vistaprint, it ships to the warehouse (AGH), and it gets
// listed like any other item. Sizes are the trimmed sheet, short side first;
// art can be either way round.
type PrintProduct struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Paper       string   `json:"paper"`  // what it's printed on, for people
	Vendor      string   `json:"vendor"` // the Vistaprint product we order
	WidthIn     float64  `json:"width_in"`
	HeightIn    float64  `json:"height_in"`
	BleedIn     float64  `json:"bleed_in"` // art runs this far past the trim on every side
	SafeIn      float64  `json:"safe_in"`  // text and logos stay this far inside the trim
	DPI         int      `json:"dpi"`      // the resolution to export at
	Finish      string   `json:"finish"`   // the default paper, as the 3D preview names it (see web/src/lib/paper.ts)
	Finishes    []string `json:"finishes"` // the papers they can pick from, the default first
	ThicknessIn float64  `json:"thickness_in"`
	Postcard    bool     `json:"postcard"` // shown as a stack of cards in 3D, rather than a sheet
	MinQuantity int      `json:"min_quantity"`
	MaxQuantity int      `json:"max_quantity"`
	Step        int      `json:"step"` // quantities come in multiples of this

	prices   []pricePoint // what Hack Club paid Vistaprint, after its ProAdvantage discount
	shipping []pricePoint // express shipping to the warehouse, up to each quantity
}

type pricePoint struct{ qty, cents int }

// PrintProducts are what staff can get made. Prices are Hack Club's own
// Vistaprint orders (order confirmations in zach@hackclub.com, 2025-05 to
// 2026-10), with the 30% ProAdvantage discount and before tax. Postcards are
// the 4x6 premium linen cards HQ always orders; posters are 11x17 flyers,
// which is how the Haven posters were ordered (same sheet, bulk pricing).
// Bleed and safe area follow Vistaprint's postcard size guide (⅛" each), and
// match the Haven postcard and poster files that printed well.
var PrintProducts = []PrintProduct{
	{
		Key: "postcard_4x6", Name: "4x6 postcard", Paper: "Premium linen card stock, printed on both sides",
		Vendor: "Vistaprint Standard Postcards, 4\" x 6\", linen", WidthIn: 4, HeightIn: 6, BleedIn: 0.125, SafeIn: 0.125,
		DPI: 300, Finish: "linen", Finishes: []string{"linen"}, ThicknessIn: 0.016, Postcard: true, MinQuantity: 50, MaxQuantity: 25000, Step: 50,
		prices: []pricePoint{
			{50, 2589}, {250, 5459}, {500, 6789}, {1000, 8259}, {1500, 13159}, {2000, 17569}, {2500, 19319},
			{3000, 22329}, {4000, 26459}, {5000, 29540}, {7000, 39620}, {10000, 53620}, {15000, 82670},
		},
		shipping: []pricePoint{{1000, 3699}, {2000, 4299}, {5000, 5299}, {10000, 7799}, {25000, 9099}},
	},
	{
		Key: "poster_11x17", Name: "11x17 poster", Paper: "Thick paper, matte or glossy",
		Vendor: "Vistaprint Flyers, 11\" x 17\", matte or glossy", WidthIn: 11, HeightIn: 17, BleedIn: 0.125, SafeIn: 0.125,
		DPI: 300, Finish: "matte", Finishes: []string{"matte", "gloss"}, ThicknessIn: 0.008, MinQuantity: 25, MaxQuantity: 5000, Step: 25,
		prices: []pricePoint{
			{25, 3499}, {100, 10000}, {500, 24709}, {1500, 51170}, {2000, 65450}, {3000, 100730},
		},
		shipping: []pricePoint{{500, 4999}, {2000, 8199}, {5000, 11299}},
	},
}

// PickFinish is the paper a request asked for: one of p's Finishes, or its
// default when they didn't say. ok is false for a paper p doesn't come on.
func (p PrintProduct) PickFinish(f string) (string, bool) {
	if f == "" {
		return p.Finish, true
	}
	for _, x := range p.Finishes {
		if x == f {
			return f, true
		}
	}
	return "", false
}

// PrintProductByKey finds a product in PrintProducts.
func PrintProductByKey(key string) (PrintProduct, bool) {
	for _, p := range PrintProducts {
		if p.Key == key {
			return p, true
		}
	}
	return PrintProduct{}, false
}

// BleedSize is the art's full size, in inches: the trim plus bleed all round.
func (p PrintProduct) BleedSize() (w, h float64) {
	return p.WidthIn + 2*p.BleedIn, p.HeightIn + 2*p.BleedIn
}

// SafeSize is the area text and logos must stay inside, in inches.
func (p PrintProduct) SafeSize() (w, h float64) {
	return p.WidthIn - 2*p.SafeIn, p.HeightIn - 2*p.SafeIn
}

// BleedPixels is the art's full size in pixels at the product's DPI.
func (p PrintProduct) BleedPixels() (w, h int) {
	bw, bh := p.BleedSize()
	return int(math.Round(bw * float64(p.DPI))), int(math.Round(bh * float64(p.DPI)))
}

// The steps from a submitted print request to stock on the warehouse's
// shelves, in business days: HQ checks the files and orders within 48 hours;
// Vistaprint prints and ships (3–7 days order to delivery on HQ's 2025-26
// orders); the warehouse receives it (as for backorders, see
// Estimate.AfterRestock).
const (
	printReviewDays     = 2
	printVendorMinDays  = 3
	printVendorMaxDays  = 7
	printReceiveMinDays = 1
	printReceiveMaxDays = 2
)

// PrintEstimate is what a print run should cost and when it should be in the
// warehouse, ready to ship. Dates are New York calendar days (YYYY-MM-DD).
type PrintEstimate struct {
	Product       string  `json:"product"`
	Quantity      int     `json:"quantity"`
	PrintCents    int     `json:"print_cents"`
	ShippingCents int     `json:"shipping_cents"`
	TotalCents    int     `json:"total_cents"`
	EachCents     float64 `json:"each_cents"`
	// The legs, in business days: HQ checks the art and orders it; Vistaprint
	// prints it and ships it to the warehouse, which receives it; and the
	// first shipment goes from the warehouse to them (WithDelivery; zero
	// without one, or without past shipments to go on).
	ReviewDays      int    `json:"review_days"`
	ToWarehouseDays [2]int `json:"to_warehouse_days"`
	ToYouDays       [2]int `json:"to_you_days"`
	// In the warehouse (MinDays–MaxDays from now), and at their door.
	MinDays    int    `json:"min_days"`
	MaxDays    int    `json:"max_days"`
	ReadyFrom  string `json:"ready_from"`
	ReadyBy    string `json:"ready_by"`
	ArriveFrom string `json:"arrive_from,omitempty"`
	ArriveBy   string `json:"arrive_by,omitempty"`
}

// EstimatePrint prices qty of p off Hack Club's past orders, reading between
// the quantities it ordered along a straight line (past the biggest, at its
// price per piece; below the smallest, the smallest order's price), and says
// when it would be in the warehouse if submitted at now.
func EstimatePrint(p PrintProduct, qty int, now time.Time) (PrintEstimate, error) {
	switch {
	case qty < p.MinQuantity || qty > p.MaxQuantity:
		return PrintEstimate{}, fmt.Errorf("order between %d and %d %ss", p.MinQuantity, p.MaxQuantity, p.Name)
	case qty%p.Step != 0:
		return PrintEstimate{}, fmt.Errorf("%ss come in multiples of %d", p.Name, p.Step)
	}
	e := PrintEstimate{Product: p.Key, Quantity: qty, PrintCents: interpolate(p.prices, qty)}
	for _, s := range p.shipping {
		e.ShippingCents = s.cents
		if qty <= s.qty {
			break
		}
	}
	e.TotalCents = e.PrintCents + e.ShippingCents
	e.EachCents = float64(e.TotalCents) / float64(qty)
	e.ReviewDays = printReviewDays
	e.ToWarehouseDays = [2]int{printVendorMinDays + printReceiveMinDays, printVendorMaxDays + printReceiveMaxDays}
	e.MinDays = e.ReviewDays + e.ToWarehouseDays[0]
	e.MaxDays = e.ReviewDays + e.ToWarehouseDays[1]
	day := newYork(now)
	e.ReadyFrom = AddBusinessDays(day, e.MinDays).Format(time.DateOnly)
	e.ReadyBy = AddBusinessDays(day, e.MaxDays).Format(time.DateOnly)
	return e, nil
}

// WithDelivery adds the first shipment's leg, from the warehouse to them:
// ship is how a package like it went before (Estimate's handling and
// transit), counted from when the run is in the warehouse.
func (e PrintEstimate) WithDelivery(ship Estimate, now time.Time) PrintEstimate {
	if ship.TransitSamples == 0 {
		return e
	}
	e.ToYouDays = [2]int{ship.HandlingDays[0] + ship.TransitDays[0], ship.HandlingDays[1] + ship.TransitDays[1]}
	day := newYork(now)
	e.ArriveFrom = AddBusinessDays(day, e.MinDays+e.ToYouDays[0]).Format(time.DateOnly)
	e.ArriveBy = AddBusinessDays(day, e.MaxDays+e.ToYouDays[1]).Format(time.DateOnly)
	return e
}

func newYork(t time.Time) time.Time {
	if ny, err := time.LoadLocation("America/New_York"); err == nil {
		return t.In(ny)
	}
	return t
}

// Item is how a run of p is listed in the shop once it's stocked under sku:
// its trimmed size (turned when the art is landscape), paper (finish, or the
// product's default), and for postcards, the shop's packs.
func (p PrintProduct) Item(sku, name, finish string, landscape bool) Item {
	w, h, t := p.WidthIn, p.HeightIn, p.ThicknessIn
	if landscape {
		w, h = h, w
	}
	if finish == "" {
		finish = p.Finish
	}
	it := Item{SKU: sku, Name: name, Visible: true, MaxPerRequest: 25, WidthIn: &w, HeightIn: &h, Finish: finish, ThicknessIn: &t}
	if p.Postcard {
		it.Packs, it.MaxPerRequest = "postcard", 100
	}
	return it
}

func interpolate(pts []pricePoint, qty int) int {
	if qty <= pts[0].qty {
		return pts[0].cents
	}
	for i := 1; i < len(pts); i++ {
		if a, b := pts[i-1], pts[i]; qty <= b.qty {
			return int(math.Round(float64(a.cents) + float64(b.cents-a.cents)*float64(qty-a.qty)/float64(b.qty-a.qty)))
		}
	}
	last := pts[len(pts)-1]
	return int(math.Round(float64(last.cents) * float64(qty) / float64(last.qty)))
}

// How print art fits a product (ArtCheck.Fit).
const (
	ArtBleed      = "bleed"       // the full size with bleed: ready to print
	ArtTrim       = "trim"        // the trimmed size: prints, but the edges may show a white sliver
	ArtWrongShape = "wrong_shape" // neither: it would be cropped or stretched
)

// artTolerance is how far art's proportions may be from the product's
// (0.5%; bleed and trim differ by 2% on a postcard): the Haven postcard's
// 1885 x 1282 px is a few pixels over 6.25 x 4.25 in at 300 DPI.
const artTolerance = 0.005

// ArtCheck says whether a piece of art (w x h pixels, either way round) will
// print well on p. Problem is set when it can't be printed as is: the wrong
// shape, or under the product's DPI (300), which Zach requires.
type ArtCheck struct {
	Fit     string `json:"fit"`
	DPI     int    `json:"dpi"`
	Problem string `json:"problem,omitempty"`
}

// CheckArt compares art's pixel size with p's full size and its trimmed size.
func CheckArt(p PrintProduct, w, h int) ArtCheck {
	short, long := float64(min(w, h)), float64(max(w, h))
	if short <= 0 {
		return ArtCheck{Fit: ArtWrongShape, Problem: "the file has no picture in it"}
	}
	bw, bh := p.BleedSize()
	pw, ph := p.BleedPixels()
	c := ArtCheck{Fit: ArtWrongShape}
	for _, size := range []struct {
		fit  string
		w, h float64
	}{{ArtBleed, bw, bh}, {ArtTrim, p.WidthIn, p.HeightIn}} {
		if math.Abs((short/long)/(size.w/size.h)-1) <= artTolerance {
			c.Fit, c.DPI = size.fit, int(short/size.w)
			break
		}
	}
	switch {
	case c.Fit == ArtWrongShape:
		c.Problem = fmt.Sprintf("it's %d x %d px, which isn't the shape of a %s: make it %d x %d px (%.2f\" x %.2f\" at %d DPI)",
			w, h, p.Name, pw, ph, bw, bh, p.DPI)
	case c.DPI < p.DPI:
		c.Problem = fmt.Sprintf("it's %d DPI, and prints need at least %d: export it at %d x %d px (%.2f\" x %.2f\" at %d DPI)",
			c.DPI, p.DPI, pw, ph, bw, bh, p.DPI)
	}
	return c
}

// TrimInset is how many pixels of bleed come off each edge of art w x h when
// it's trimmed: none when the art is already the trimmed size.
func TrimInset(p PrintProduct, w, h int) int {
	if CheckArt(p, w, h).Fit != ArtBleed {
		return 0
	}
	bw, _ := p.BleedSize()
	return int(float64(min(w, h)) * p.BleedIn / bw)
}
