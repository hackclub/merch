package swag

import (
	"math"
	"testing"
	"time"
)

// card makes one parcel to country at each whole-ounce weight in ws, priced
// base + perOz a whole ounce, shipped on day.
func card(country string, day time.Time, base, perOz float64, ws ...float64) []PastShipment {
	var out []PastShipment
	for _, w := range ws {
		out = append(out, PastShipment{Country: country, Class: ClassParcel, WeightOz: w - 0.4,
			PostageCents: int(math.Round(base + perOz*w)), ShippedOn: day})
	}
	return out
}

func packedAt(oz float64) Packing {
	return Packing{Package: Package{Name: "Bubble mailer", Class: ClassParcel}, WeightOz: oz}
}

var aug = time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)

// Asendia prices each country by the whole ounce: a base plus so much an
// ounce (Canada in 2026: $8.90 + $0.414/oz). Weights between the ones shipped
// before read off the line between them.
func TestEstimateReadsTheRateCard(t *testing.T) {
	h := card("CA", aug, 890, 41.4, 2, 4, 9, 13)
	for _, c := range []struct {
		oz   float64
		want int
	}{
		{4, 1056},    // exactly 4 oz is the 4 oz price
		{4.01, 1097}, // a hair over goes to 5 oz
		{6.5, 1180},  // 7 oz, between 4 and 9
		{20, 1718},   // past the heaviest: the country's own per-ounce rate
	} {
		e := EstimateShipping(h, "CA", packedAt(c.oz), now)
		if e.PostageTypicalCents != c.want || e.PostageBasis != "CA" || e.FeeCents != 0 {
			t.Errorf("%v oz: postage %d (%s), want %d", c.oz, e.PostageTypicalCents, e.PostageBasis, c.want)
		}
	}
}

// Rates rise every January (5.3% in 2025, 15% in 2026). Once a few prices
// show the rise, it carries over to weights only shipped at the old prices.
func TestEstimateCarriesRateIncreases(t *testing.T) {
	old := time.Date(2025, 11, 10, 0, 0, 0, 0, time.UTC)
	jan := time.Date(2026, 1, 12, 0, 0, 0, 0, time.UTC)
	var h []PastShipment
	h = append(h, card("IN", old, 651, 50.8, 2, 4, 5, 9, 12, 13)...)
	h = append(h, card("GB", old, 548, 27.5, 2, 4, 5, 9)...)
	h = append(h, card("CA", old, 890, 41.4, 2, 4, 5, 9)...)
	for _, s := range card("IN", jan, 651, 50.8, 2, 4) {
		s.PostageCents = int(math.Round(float64(s.PostageCents) * 1.15))
		h = append(h, s)
	}
	for _, s := range append(card("GB", jan, 548, 27.5, 4, 5), card("CA", jan, 890, 41.4, 5, 9)...) {
		s.PostageCents = int(math.Round(float64(s.PostageCents) * 1.15))
		h = append(h, s)
	}
	e := EstimateShipping(h, "IN", packedAt(11.6), now) // 12 oz, last shipped at the old price
	if want := 1.15 * (651 + 50.8*12); math.Abs(float64(e.PostageTypicalCents)-want) > 2 {
		t.Fatalf("postage %d, want about %.0f", e.PostageTypicalCents, want)
	}
}

// One odd price (a parcel that went by another service) doesn't move the card.
func TestEstimateIgnoresAnOddPrice(t *testing.T) {
	h := card("IN", aug, 651, 50.8, 2, 4, 5, 9, 13, 16, 21)
	for i := range h {
		if h[i].WeightOz > 20 {
			h[i].PostageCents *= 4
		}
	}
	e := EstimateShipping(h, "IN", packedAt(20.5), now)
	if want := 651 + 50.8*21; math.Abs(float64(e.PostageTypicalCents)-want) > 2 {
		t.Fatalf("postage %d, want about %.0f", e.PostageTypicalCents, want)
	}
}

// Letters abroad go by the ounce too: $1.93 up to 1 oz, $3.85 up to 2 oz.
func TestEstimateLettersByTheOunce(t *testing.T) {
	var h []PastShipment
	for range 5 {
		h = append(h, PastShipment{Country: "DE", Class: ClassLetter, WeightOz: 0.6, PostageCents: 193, ShippedOn: aug},
			PastShipment{Country: "DE", Class: ClassLetter, WeightOz: 1.6, PostageCents: 385, ShippedOn: aug})
	}
	for _, c := range []struct {
		oz   float64
		want int
	}{{0.9, 193}, {1.3, 385}} {
		p := Packing{Package: Package{Name: "Envelope", Class: ClassLetter}, WeightOz: c.oz}
		if e := EstimateShipping(h, "DE", p, now); e.PostageTypicalCents != c.want {
			t.Errorf("%v oz letter: postage %d, want %d", c.oz, e.PostageTypicalCents, c.want)
		}
	}
}

// A country with too little history goes by parcels of the same weight to
// anywhere abroad.
func TestEstimateRateCardFallsBackToAllInternational(t *testing.T) {
	var h []PastShipment
	h = append(h, card("GB", aug, 548, 27.5, 2, 4, 6, 8)...)
	h = append(h, card("CA", aug, 890, 41.4, 2, 4, 6, 8)...)
	h = append(h, card("PE", aug, 1500, 60, 6)...)
	e := EstimateShipping(h, "PE", packedAt(5.5), now)
	if e.PostageBasis != "international" || e.PostageTypicalCents == 0 {
		t.Fatalf("got %+v", e)
	}
}

// The dearest countries' prices bend over a pound: Colombia went up $1.45 an
// ounce to 20 oz, then hardly at all to 39 oz. Past the heaviest weight, a card
// follows its heavier half, so a 3 lb parcel there isn't priced off the steep
// light end ($90 on 2026-10-18, against a 39 oz label of about $45).
func TestEstimateFollowsWhereTheCardBends(t *testing.T) {
	h := card("CO", aug, 700, 160, 1, 2, 5, 8, 9, 20)
	h = append(h, card("CO", aug, 3900, 10, 39)...)
	e := EstimateShipping(h, "CO", packedAt(48), now)
	if e.PostageTypicalCents < 3900 || e.PostageTypicalCents > 4500 {
		t.Fatalf("48 oz to CO = %d, want about the 39 oz price (4290) and a little more", e.PostageTypicalCents)
	}
	// A straight card carries on at its own rate.
	h = card("IN", aug, 650, 51, 1, 2, 4, 8, 12, 20)
	if e := EstimateShipping(h, "IN", packedAt(40), now); e.PostageTypicalCents != 650+51*40 {
		t.Fatalf("40 oz to IN = %d, want %d", e.PostageTypicalCents, 650+51*40)
	}
}
