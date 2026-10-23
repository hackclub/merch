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
	// 48 hours to check the art and order, then Vistaprint prints and ships and the warehouse receives it.
	if e.ReviewDays != 2 || e.ToWarehouseDays != [2]int{4, 9} || e.MinDays != 6 || e.MaxDays != 11 {
		t.Fatalf("days = %d + %v = %d–%d", e.ReviewDays, e.ToWarehouseDays, e.MinDays, e.MaxDays)
	}
	if e.ReadyFrom != "2026-11-02" || e.ReadyBy != "2026-11-09" {
		t.Fatalf("ready %s – %s", e.ReadyFrom, e.ReadyBy)
	}
	// Then the first shipment goes out like any request: the warehouse's handling and the mail.
	d := e.WithDelivery(Estimate{HandlingDays: [2]int{1, 2}, TransitDays: [2]int{3, 5}, TransitSamples: 12}, now)
	if d.ToYouDays != [2]int{4, 7} || d.ArriveFrom != "2026-11-06" || d.ArriveBy != "2026-11-18" {
		t.Fatalf("delivery = %v, %s – %s", d.ToYouDays, d.ArriveFrom, d.ArriveBy)
	}
	// Without past shipments to go on, there's no last leg to add.
	if d := e.WithDelivery(Estimate{}, now); d.ToYouDays != [2]int{} || d.ArriveBy != "" {
		t.Fatalf("no history = %+v", d)
	}
}

func TestPrintProductItem(t *testing.T) {
	pc, _ := PrintProductByKey("postcard_4x6")
	it := pc.Item("Pri/Hav/4x6/1st", "Haven postcard", "", true)
	if it.Name != "Haven postcard" || it.SKU != "Pri/Hav/4x6/1st" || *it.WidthIn != 6 || *it.HeightIn != 4 || *it.ThicknessIn != 0.016 ||
		it.Packs != "postcard" || it.MaxPerRequest != 100 || it.Finish != "linen" || !it.Visible {
		t.Fatalf("postcard item = %+v", it)
	}
	poster, _ := PrintProductByKey("poster_11x17")
	if it := poster.Item("", "Sprig poster", "", false); *it.WidthIn != 11 || *it.HeightIn != 17 || it.Packs != "" || it.MaxPerRequest != 25 || it.Finish != "matte" {
		t.Fatalf("poster item = %+v", it)
	}
	if it := poster.Item("", "Sprig poster", "gloss", false); it.Finish != "gloss" {
		t.Fatalf("glossy poster item = %+v", it)
	}
}

func TestPrintFinishes(t *testing.T) {
	pc, _ := PrintProductByKey("postcard_4x6")
	poster, _ := PrintProductByKey("poster_11x17")
	// Posters come matte (the default, how the Haven posters were printed) or glossy; postcards only on linen.
	if len(poster.Finishes) != 2 || poster.Finishes[0] != "matte" || poster.Finishes[1] != "gloss" || poster.Finish != "matte" {
		t.Fatalf("poster finishes = %v, default %q", poster.Finishes, poster.Finish)
	}
	for _, tc := range []struct {
		p    PrintProduct
		in   string
		want string
		ok   bool
	}{{poster, "", "matte", true}, {poster, "gloss", "gloss", true}, {poster, "linen", "", false}, {pc, "", "linen", true}, {pc, "gloss", "", false}} {
		if got, ok := tc.p.PickFinish(tc.in); got != tc.want || ok != tc.ok {
			t.Errorf("%s PickFinish(%q) = %q, %v; want %q, %v", tc.p.Key, tc.in, got, ok, tc.want, tc.ok)
		}
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
