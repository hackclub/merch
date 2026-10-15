package swag

import (
	"errors"
	"fmt"
	"math"
)

// Shipping classes: how the warehouse sends a package, which decides its
// carrier, cost and speed. Stickers in an envelope go as USPS First Class letter
// mail, in the US and abroad. Everything else is a parcel: USPS Ground
// Advantage or UPS SurePost in the US, and Asendia abroad (up to 3 lb).
const (
	ClassLetter = "letter"
	ClassParcel = "parcel"
)

// MaxInternationalOz is the most an international package can weigh, packed:
// Asendia, our international carrier, takes up to 3 lb.
const MaxInternationalOz = 48

// PaperOzPerCubicInch turns a printed item's size into a weight when it has
// none set. Measured from the warehouse's 4x6 postcards (0.24 oz each); it gets
// the Mini-Magazine and Magazine 2025 within 10-20% of their shipped weights.
const PaperOzPerCubicInch = 0.625

// Package is a mailer the warehouse packs requests in. Sizes are inside sizes.
type Package struct {
	Name     string  `json:"name"`
	Class    string  `json:"class"`
	WidthIn  float64 `json:"width_in"`
	HeightIn float64 `json:"height_in"`
	DepthIn  float64 `json:"depth_in"` // how thick the contents can be
	MaxOz    float64 `json:"max_oz"`   // heaviest it can be, packed
	TareOz   float64 `json:"tare_oz"`  // the packaging itself
	// CostCents is what one costs Hack Club: AGH's invoice rate, or Uline's
	// price for what we stock there ourselves.
	CostCents int `json:"cost_cents"`
	// Piles reports whether contents can sit side by side. A StayFlat is one
	// stack, poster on the bottom.
	Piles bool `json:"-"`
	// KeepsFlat marks rigid flat mailers (StayFlats), the only packaging big
	// prints like posters ship in.
	KeepsFlat bool `json:"-"`
}

// cartonOz is a corrugated box's weight: Uline's 10x8x6 (S-4103) weighs
// 0.44 lb for 376 sq in of board, about 0.0187 oz per sq in.
func cartonOz(l, w, h float64) float64 { return 0.0187 * 2 * (l*w + l*h + w*h) }

func carton(l, w, h float64, cents int) Package {
	return Package{Name: fmt.Sprintf("%gx%gx%g box", l, w, h), Class: ClassParcel, WidthIn: l, HeightIn: w, DepthIn: h,
		MaxOz: 800, TareOz: math.Round(cartonOz(l, w, h)*10) / 10, CostCents: cents, Piles: true}
}

// Packages are what AGH, our warehouse, packs requests in, in the order a
// request tries them; it ships in the first one everything fits in. They come
// from AGH's invoices (bubble mailers, the 6x4x2 box mailers and the cartons
// they bill) and the Uline Black Self-Seal Stayflats we stock there (sizes are
// Uline's usable sizes; weights are a case's weight over 100). Costs are AGH's
// invoice rates (2024-2026), what we paid for the 6x4x2 box mailers (1,000 for
// $708.76 in 2025), and Uline's carton price over 100 for the StayFlats
// (2026-10-15). AGH doesn't bill for envelopes.
var Packages = []Package{
	// #6x9 envelope: First Class letter mail, max 1/4" thick and 3.5 oz.
	{Name: "Envelope", Class: ClassLetter, WidthIn: 5.75, HeightIn: 8.75, DepthIn: 0.25, MaxOz: 3.5, TareOz: 0.3, Piles: true},
	// Uline S-11837 (#5, 9 3/4 x 12 1/4), S-11838 (#3, 11 x 13 1/2) and
	// S-11839 (#6, 13 x 18): rigid, for contents up to 1/4" thick.
	{Name: "9x12 StayFlat", Class: ClassParcel, WidthIn: 9.25, HeightIn: 12, DepthIn: 0.25, MaxOz: 48, TareOz: 3.5, CostCents: 95, KeepsFlat: true},
	{Name: "11x13 StayFlat", Class: ClassParcel, WidthIn: 10.5, HeightIn: 13.25, DepthIn: 0.25, MaxOz: 48, TareOz: 4.3, CostCents: 106, KeepsFlat: true},
	{Name: "13x18 StayFlat", Class: ClassParcel, WidthIn: 12.5, HeightIn: 17.75, DepthIn: 0.25, MaxOz: 64, TareOz: 6.7, CostCents: 148, KeepsFlat: true},
	// AGH's invoices don't give the bubble mailers' size; this is a standard #4.
	{Name: "Bubble mailer", Class: ClassParcel, WidthIn: 9, HeightIn: 13.5, DepthIn: 1.5, MaxOz: 64, TareOz: 1, CostCents: 50, Piles: true},
	{Name: "6x4x2 box mailer", Class: ClassParcel, WidthIn: 6, HeightIn: 4, DepthIn: 2, MaxOz: 160, TareOz: 2, CostCents: 71, Piles: true},
	carton(8, 6, 6, 60),
	carton(10, 8, 6, 70),
	carton(13, 10, 4, 80),
	carton(14, 10, 6, 100),
	carton(18, 12, 6, 125),
	carton(18, 14, 10, 200),
}

var (
	ErrWontPack = errors.New("these won't ship together in one package")
	ErrTooHeavy = errors.New("international requests can weigh at most 3 lb packed")
)

// Packing is how a request goes out: its package and weight.
type Packing struct {
	Package Package `json:"package"`
	// WeightOz is what it really weighs, packaging included (for the 3 lb
	// limit). ContentsOz leaves the packaging out, like the warehouse's own
	// logged weights, which come from per-SKU weights (see EstimateShipping).
	WeightOz   float64 `json:"weight_oz"`
	ContentsOz float64 `json:"contents_oz"`
	// UnknownWeight is set when some item has no weight and no paper size to
	// work one out from, so WeightOz is too low.
	UnknownWeight bool `json:"unknown_weight"`
}

// shape is one request line as a stack of identical things.
type shape struct {
	name    string
	w, h, t float64 // inches; t is one unit's thickness
	qty     int
	oz      float64 // one unit
	known   bool
}

// Sizes for things the catalog doesn't give a size: stickers, postcards, and
// anything else (a folded T-shirt, a gadget), which is treated as bulky.
var (
	stickerSize  = [3]float64{3, 3, 0.01}
	postcardSize = [3]float64{4, 6, 0.016}
	bulkySize    = [3]float64{10, 8, 1}
)

func shapeOf(it Item, qty int) shape {
	s := shape{name: it.Name, qty: qty}
	switch {
	case it.WidthIn != nil && it.HeightIn != nil && it.ThicknessIn != nil:
		s.w, s.h, s.t = *it.WidthIn, *it.HeightIn, *it.ThicknessIn
	case it.Packs == "sticker":
		s.w, s.h, s.t = stickerSize[0], stickerSize[1], stickerSize[2]
	case it.Packs == "postcard":
		s.w, s.h, s.t = postcardSize[0], postcardSize[1], postcardSize[2]
	default:
		s.w, s.h, s.t = bulkySize[0], bulkySize[1], bulkySize[2]
	}
	if it.Binding == "trifold" {
		// A trifold ships folded in thirds: a third as wide, three sheets thick.
		s.w, s.t = s.w/3, s.t*3
	}
	switch {
	case it.WeightOz != nil:
		s.oz, s.known = *it.WeightOz, true
	case it.WidthIn != nil && it.HeightIn != nil && it.ThicknessIn != nil:
		s.oz, s.known = s.w*s.h*s.t*PaperOzPerCubicInch, true
	}
	return s
}

// bigSheet reports whether s is a big print (a poster), which ships only in a
// StayFlat so it doesn't bend.
func (s shape) bigSheet() bool { return s.t <= 0.02 && max(s.w, s.h) > 13.5 }

// piles is how many stacks of s fit side by side in p (0 if s doesn't fit).
func (p Package) piles(s shape) int {
	if s.bigSheet() && !p.KeepsFlat {
		return 0
	}
	n := 0
	for _, o := range [][2]float64{{s.w, s.h}, {s.h, s.w}} {
		if o[0] <= p.WidthIn && o[1] <= p.HeightIn {
			n = max(n, int(p.WidthIn/o[0])*int(p.HeightIn/o[1]))
		}
	}
	if n > 0 && !p.Piles {
		return 1
	}
	return n
}

// fits reports whether every shape goes in p together, ignoring weight.
func (p Package) fits(shapes []shape) bool {
	var stack, volume float64
	for _, s := range shapes {
		n := p.piles(s)
		if n == 0 {
			return false
		}
		h := math.Ceil(float64(s.qty)/float64(n)) * s.t
		if h > p.DepthIn+1e-9 {
			return false
		}
		stack += h
		volume += float64(s.qty) * s.w * s.h * s.t
	}
	if !p.Piles {
		return stack <= p.DepthIn+1e-9
	}
	// Leave room for things not stacking perfectly.
	return volume <= 0.8*p.WidthIn*p.HeightIn*p.DepthIn
}

// Pack picks the package a request ships in. It refuses requests that don't
// fit in one package (ErrWontPack), and international ones over 3 lb
// (ErrTooHeavy). Lines must name items in the catalog.
func Pack(lines []Line, catalog map[int64]Item, international bool) (Packing, error) {
	var shapes []shape
	var contentOz float64
	known := true
	for _, l := range lines {
		s := shapeOf(catalog[l.ItemID], l.Quantity)
		shapes = append(shapes, s)
		contentOz += s.oz * float64(s.qty)
		known = known && s.known
	}
	tooHeavy := false
	for _, p := range Packages {
		if !p.fits(shapes) {
			continue
		}
		oz := contentOz + p.TareOz
		if oz > p.MaxOz {
			continue
		}
		if international && oz > MaxInternationalOz {
			tooHeavy = true
			continue
		}
		return Packing{Package: p, WeightOz: oz, ContentsOz: contentOz, UnknownWeight: !known}, nil
	}
	if tooHeavy {
		return Packing{}, fmt.Errorf("%w; this is about %.1f lb", ErrTooHeavy, contentOz/16)
	}
	// Name the big print the rest won't go in with, if there is one.
	for _, s := range shapes {
		if s.bigSheet() {
			return Packing{}, fmt.Errorf("%w: the %s ships flat in a 13x18 StayFlat, which holds a stack no more than 1/4 inch thick; a few stickers fit with it, but request the rest separately", ErrWontPack, s.name)
		}
	}
	return Packing{}, fmt.Errorf("%w: that's more than fits in one box; ask for fewer", ErrWontPack)
}
