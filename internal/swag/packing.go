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
	// Piles reports whether contents can sit side by side. A flat poster mailer
	// is one stack on top of the poster.
	Piles bool `json:"-"`
}

// Packages are the mailers a request can go out in, cheapest first. A request
// ships in the first one everything fits in. These are standard sizes, not the
// warehouse's (AGH's) exact stock; tune them here.
var Packages = []Package{
	// #6x9 envelope: First Class letter mail, max 1/4" thick and 3.5 oz.
	{Name: "Envelope", Class: ClassLetter, WidthIn: 5.75, HeightIn: 8.75, DepthIn: 0.25, MaxOz: 3.5, TareOz: 0.3, Piles: true},
	{Name: "Mailer", Class: ClassParcel, WidthIn: 9.5, HeightIn: 12.5, DepthIn: 2, MaxOz: 320, TareOz: 2, Piles: true},
	{Name: "Poster mailer", Class: ClassParcel, WidthIn: 12, HeightIn: 18, DepthIn: 0.25, MaxOz: 64, TareOz: 4},
	{Name: "Box", Class: ClassParcel, WidthIn: 12, HeightIn: 10, DepthIn: 4, MaxOz: 640, TareOz: 8, Piles: true},
	// Too small for a poster on purpose: posters only ship flat in a poster mailer.
	{Name: "Large box", Class: ClassParcel, WidthIn: 16, HeightIn: 12, DepthIn: 8, MaxOz: 800, TareOz: 16, Piles: true},
}

var (
	ErrWontPack = errors.New("these won't ship together in one package")
	ErrTooHeavy = errors.New("international requests can weigh at most 3 lb packed")
)

// Packing is how a request goes out: its package and packed weight.
type Packing struct {
	Package  Package `json:"package"`
	WeightOz float64 `json:"weight_oz"`
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
	switch {
	case it.WeightOz != nil:
		s.oz, s.known = *it.WeightOz, true
	case it.WidthIn != nil && it.HeightIn != nil && it.ThicknessIn != nil:
		s.oz, s.known = s.w*s.h*s.t*PaperOzPerCubicInch, true
	}
	return s
}

// piles is how many stacks of s fit side by side in p (0 if s doesn't fit).
func (p Package) piles(s shape) int {
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
		return Packing{Package: p, WeightOz: oz, UnknownWeight: !known}, nil
	}
	if tooHeavy {
		return Packing{}, fmt.Errorf("%w; this is about %.1f lb", ErrTooHeavy, contentOz/16)
	}
	// Name the item that forces a flat poster mailer, if one does: it's what
	// the rest won't go in with.
	for _, s := range shapes {
		if onlyFits(s) == "Poster mailer" {
			return Packing{}, fmt.Errorf("%w: the %s ships flat in a poster mailer, which only has room for a few thin things like stickers beside it; request the rest separately", ErrWontPack, s.name)
		}
	}
	return Packing{}, fmt.Errorf("%w: that's more than fits in one box; ask for fewer", ErrWontPack)
}

// onlyFits names the one package s fits in, if there's just one.
func onlyFits(s shape) string {
	name := ""
	for _, p := range Packages {
		if p.piles(s) > 0 {
			if name != "" {
				return ""
			}
			name = p.Name
		}
	}
	return name
}
