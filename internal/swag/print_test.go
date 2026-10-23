package swag

import (
	"testing"
	"time"
)

func TestPrintProducts(t *testing.T) {
	pc, ok := PrintProductByKey("postcard_4x6")
	if !ok || pc.WidthIn != 4 || pc.HeightIn != 6 || pc.BleedIn != 0.125 || pc.SafeIn != 0.125 || pc.DPI != 300 || pc.Finish != "linen" {
		t.Fatalf("postcard = %+v", pc)
	}
	if w, h := pc.BleedSize(); w != 4.25 || h != 6.25 {
		t.Fatalf("postcard bleed size = %v x %v", w, h)
	}
	if w, h := pc.SafeSize(); w != 3.75 || h != 5.75 {
		t.Fatalf("postcard safe size = %v x %v", w, h)
	}
	if w, h := pc.BleedPixels(); w != 1275 || h != 1875 {
		t.Fatalf("postcard bleed pixels = %d x %d", w, h)
	}
	po, ok := PrintProductByKey("poster_11x17")
	if !ok || po.WidthIn != 11 || po.HeightIn != 17 {
		t.Fatalf("poster = %+v", po)
	}
	if w, h := po.BleedPixels(); w != 3375 || h != 5175 {
		t.Fatalf("poster bleed pixels = %d x %d", w, h)
	}
	if _, ok := PrintProductByKey("banner"); ok {
		t.Fatal("unknown product found")
	}
}

func TestEstimatePrintPrices(t *testing.T) {
	pc, _ := PrintProductByKey("postcard_4x6")
	po, _ := PrintProductByKey("poster_11x17")
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC) // a Friday
	for _, tc := range []struct {
		p     PrintProduct
		qty   int
		print int
	}{
		{pc, 1000, 8259},    // Hack Club's order of 2026-08-14
		{pc, 5000, 29540},   // 2026-08-29
		{pc, 3500, 24394},   // halfway between 3,000 ($223.29) and 4,000 ($264.59)
		{pc, 20000, 110227}, // past 15,000: its price per card
		{po, 3000, 100730},  // the Haven posters, 2026-10-17
		{po, 100, 10000},
	} {
		e, err := EstimatePrint(tc.p, tc.qty, now)
		if err != nil {
			t.Fatalf("%s × %d: %v", tc.p.Key, tc.qty, err)
		}
		if e.PrintCents != tc.print || e.TotalCents != e.PrintCents+e.ShippingCents || e.ShippingCents <= 0 {
			t.Errorf("%s × %d = %+v, want printing %d", tc.p.Key, tc.qty, e, tc.print)
		}
		if e.EachCents != float64(e.TotalCents)/float64(tc.qty) {
			t.Errorf("%s × %d each = %v", tc.p.Key, tc.qty, e.EachCents)
		}
	}
	// More never costs less.
	for _, p := range PrintProducts {
		last := 0
		for q := p.MinQuantity; q <= p.MaxQuantity; q += p.Step {
			e, _ := EstimatePrint(p, q, now)
			if e.TotalCents < last {
				t.Fatalf("%s × %d costs %d, less than %d for fewer", p.Key, q, e.TotalCents, last)
			}
			last = e.TotalCents
		}
	}
}

func TestEstimatePrintQuantityLimits(t *testing.T) {
	pc, _ := PrintProductByKey("postcard_4x6")
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	for _, q := range []int{0, pc.MinQuantity - 1, pc.MaxQuantity + pc.Step, pc.MinQuantity + 1} {
		if _, err := EstimatePrint(pc, q, now); err == nil {
			t.Errorf("quantity %d: no error", q)
		}
	}
}

func TestEstimatePrintTimeline(t *testing.T) {
	pc, _ := PrintProductByKey("postcard_4x6")
	// Friday, 2026-10-23, in New York.
	now := time.Date(2026, 10, 23, 16, 0, 0, 0, time.UTC)
	e, _ := EstimatePrint(pc, 1000, now)
	if e.MinDays != 5 || e.MaxDays != 10 {
		t.Fatalf("days = %d–%d", e.MinDays, e.MaxDays)
	}
	if e.ReadyFrom != "2026-10-30" || e.ReadyBy != "2026-11-06" {
		t.Fatalf("ready %s – %s", e.ReadyFrom, e.ReadyBy)
	}
}

func TestCheckArt(t *testing.T) {
	pc, _ := PrintProductByKey("postcard_4x6")
	for _, tc := range []struct {
		name    string
		w, h    int
		fit     string
		dpi     int
		problem bool
	}{
		{"portrait with bleed", 1275, 1875, ArtBleed, 300, false},
		{"landscape with bleed", 1875, 1275, ArtBleed, 300, false},
		{"the Haven postcard file", 1885, 1282, ArtBleed, 301, false},
		{"trim size, no bleed", 3000, 2000, ArtTrim, 500, false},
		{"square", 2000, 2000, ArtWrongShape, 0, true},
		{"too small", 425, 625, ArtBleed, 100, true},
		{"a pixel under 300 DPI", 1274, 1874, ArtBleed, 299, true},
		{"trim size at 200 DPI", 800, 1200, ArtTrim, 200, true},
	} {
		c := CheckArt(pc, tc.w, tc.h)
		if c.Fit != tc.fit || c.DPI != tc.dpi || (c.Problem != "") != tc.problem {
			t.Errorf("%s: %+v", tc.name, c)
		}
	}
}

func TestTrimInset(t *testing.T) {
	pc, _ := PrintProductByKey("postcard_4x6")
	// ⅛" of a 4.25" side: the bleed comes off every edge.
	if got := TrimInset(pc, 1875, 1275); got != 37 {
		t.Fatalf("inset = %d, want 37 px", got)
	}
	// Art at the trimmed size has no bleed to cut.
	if got := TrimInset(pc, 3000, 2000); got != 0 {
		t.Fatalf("trim-size inset = %d", got)
	}
}
