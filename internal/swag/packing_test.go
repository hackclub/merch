package swag

import (
	"errors"
	"math"
	"testing"
)

func f(v float64) *float64 { return &v }

// shopCatalog mirrors the real shop: printed items have their real sizes, and
// weights come from past warehouse shipments.
func shopCatalog() map[int64]Item {
	return map[int64]Item{
		1: {ID: 1, SKU: "Pri/HPd/11x/1st", Name: "11x17 Hackpad Poster", Visible: true, MaxPerRequest: 20,
			WidthIn: f(11), HeightIn: f(17), ThicknessIn: f(0.008)},
		2: {ID: 2, SKU: "Pri/Wel/4x6/1st", Name: "Welcome Postcard", Visible: true, MaxPerRequest: 150, Packs: "postcard",
			WidthIn: f(6), HeightIn: f(4), ThicknessIn: f(0.016)},
		3: {ID: 3, SKU: "Sti/Sti/Kaw/1st", Name: "Kawaii Sticker", Visible: true, MaxPerRequest: 150, Packs: "sticker", WeightOz: f(0.05)},
		4: {ID: 4, SKU: "Pri/Bok/M25/1st", Name: "Magazine 2025", Visible: true, MaxPerRequest: 25,
			WidthIn: f(5.83), HeightIn: f(8.27), ThicknessIn: f(0.3), Binding: "perfect", WeightOz: f(10.8)},
		5: {ID: 5, SKU: "Pri/Bok/Mini25/1st", Name: "Mini-Magazine", Visible: true, MaxPerRequest: 100, Packs: "fives",
			WidthIn: f(6), HeightIn: f(6), ThicknessIn: f(0.04), Binding: "staple", WeightOz: f(1)},
		6: {ID: 6, SKU: "Swa/Shirt/HC", Name: "T-Shirt", Visible: true, MaxPerRequest: 1, WeightOz: f(6.5),
			Variants: []Variant{{Label: "M", SKU: "Swa/Shirt/HC/M"}}},
		7: {ID: 7, SKU: "Pri/Hac/Tea/T26", Name: "Teacher Trifold Brochure", Visible: true, MaxPerRequest: 50,
			WidthIn: f(11), HeightIn: f(8.5), ThicknessIn: f(0.005), Binding: "trifold"},
	}
}

func TestPack(t *testing.T) {
	cases := []struct {
		name    string
		lines   []Line
		intl    bool
		want    string // package name
		wantErr error
	}{
		{"stickers go in an envelope", []Line{{ItemID: 3, Quantity: 10}}, false, "Envelope", nil},
		{"stickers and a few postcards still fit an envelope", []Line{{ItemID: 3, Quantity: 3}, {ItemID: 2, Quantity: 5}}, true, "Envelope", nil},
		{"a magazine is too thick for a StayFlat", []Line{{ItemID: 4, Quantity: 1}}, false, "Bubble mailer", nil},
		{"a shirt and stickers share a mailer", []Line{{ItemID: 6, Quantity: 1, SKU: "Swa/Shirt/HC/M"}, {ItemID: 3, Quantity: 10}}, true, "Bubble mailer", nil},
		{"a poster ships flat", []Line{{ItemID: 1, Quantity: 1}}, false, "13x18 StayFlat", nil},
		{"stickers ride along with a poster", []Line{{ItemID: 1, Quantity: 2}, {ItemID: 3, Quantity: 10}}, true, "13x18 StayFlat", nil},
		{"a poster and a stack of postcards don't pack together", []Line{{ItemID: 1, Quantity: 1}, {ItemID: 2, Quantity: 50}}, false, "", ErrWontPack},
		{"a poster and a magazine don't pack together", []Line{{ItemID: 1, Quantity: 1}, {ItemID: 4, Quantity: 1}}, false, "", ErrWontPack},
		{"a club's magazines go in a box", []Line{{ItemID: 4, Quantity: 25}}, false, "13x10x4 box", nil},
		{"100 magazines fill the biggest box, under 70 lb", []Line{{ItemID: 4, Quantity: 100}}, false, "18x14x10 box", nil},
		{"110 magazines are more than a box holds", []Line{{ItemID: 4, Quantity: 110}}, false, "", ErrWontPack},
		{"four magazines are under 3 lb abroad", []Line{{ItemID: 4, Quantity: 4}}, true, "Bubble mailer", nil},
		{"five magazines are over 3 lb abroad", []Line{{ItemID: 4, Quantity: 5}}, true, "", ErrTooHeavy},
		{"five magazines are fine in the US", []Line{{ItemID: 4, Quantity: 5}}, false, "Bubble mailer", nil},
		{"45 mini-magazines make 3 lb", []Line{{ItemID: 5, Quantity: 45}}, true, "Bubble mailer", nil},
		{"a few prints go in a StayFlat", []Line{{ItemID: 2, Quantity: 10}, {ItemID: 3, Quantity: 3}, {ItemID: 5, Quantity: 1}}, false, "9x12 StayFlat", nil},
		{"a poster never goes in a box", []Line{{ItemID: 1, Quantity: 1}, {ItemID: 6, Quantity: 1, SKU: "Swa/Shirt/HC/M"}}, false, "", ErrWontPack},
		{"50 mini-magazines don't", []Line{{ItemID: 5, Quantity: 50}}, true, "", ErrTooHeavy},
		{"a trifold brochure ships folded, so it fits an envelope", []Line{{ItemID: 7, Quantity: 1}}, false, "Envelope", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := Pack(c.lines, shopCatalog(), c.intl)
			if c.wantErr != nil {
				if !errors.Is(err, c.wantErr) {
					t.Fatalf("Pack = %+v, %v; want %v", p, err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Pack: %v", err)
			}
			if p.Package.Name != c.want {
				t.Fatalf("package = %q (%.1f oz), want %q", p.Package.Name, p.WeightOz, c.want)
			}
		})
	}
}

func TestPackWeight(t *testing.T) {
	// 4 magazines (43.2 oz) in a bubble mailer (1 oz).
	p, err := Pack([]Line{{ItemID: 4, Quantity: 4}}, shopCatalog(), true)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(p.WeightOz-44.2) > 0.01 || math.Abs(p.ContentsOz-43.2) > 0.01 || p.Package.Class != ClassParcel {
		t.Fatalf("got %+v", p)
	}
	// A postcard has no weight set: it's worked out from its paper (4x6x0.016 in).
	p, err = Pack([]Line{{ItemID: 2, Quantity: 10}}, shopCatalog(), false)
	if err != nil {
		t.Fatal(err)
	}
	if want := 10*4*6*0.016*PaperOzPerCubicInch + p.Package.TareOz; math.Abs(p.WeightOz-want) > 0.01 {
		t.Fatalf("weight = %.2f, want %.2f", p.WeightOz, want)
	}
	if p.UnknownWeight {
		t.Fatal("postcards have a known (paper) weight")
	}
	// Something with no size or weight still packs, flagged.
	cat := map[int64]Item{9: {ID: 9, SKU: "Har/X", Name: "Gadget", Visible: true, MaxPerRequest: 1}}
	p, err = Pack([]Line{{ItemID: 9, Quantity: 1}}, cat, false)
	if err != nil || !p.UnknownWeight {
		t.Fatalf("got %+v, %v", p, err)
	}
}

func TestValidateRefusesWhatWontPack(t *testing.T) {
	st := settings()
	st.MaxItemsPerRequest = 0
	in := ValidateInput{Settings: st, Catalog: shopCatalog(), Purpose: PurposeClub, Reason: "club",
		Lines: []Line{{ItemID: 1, Quantity: 1}, {ItemID: 2, Quantity: 50}}, Now: now}
	if err := Validate(in); !errors.Is(err, ErrWontPack) {
		t.Fatalf("Validate = %v, want ErrWontPack", err)
	}
	in.Lines = []Line{{ItemID: 4, Quantity: 5}}
	if err := Validate(in); err != nil {
		t.Fatalf("5 magazines in the US: %v", err)
	}
	in.Country = "CA"
	if err := Validate(in); !errors.Is(err, ErrTooHeavy) {
		t.Fatalf("5 magazines to Canada = %v, want ErrTooHeavy", err)
	}
}
