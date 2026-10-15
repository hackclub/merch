package swag

import (
	"testing"
	"time"
)

func days(d float64) *float64 { return &d }

// past makes n shipments to country, with handling, transit and postage
// spread evenly from lo to hi.
func past(n int, country, class string, oz float64, handling, transit, postage [2]float64) []PastShipment {
	var out []PastShipment
	for i := range n {
		at := func(r [2]float64) float64 { return r[0] + (r[1]-r[0])*float64(i)/float64(max(n-1, 1)) }
		out = append(out, PastShipment{
			Country: country, Class: class, WeightOz: oz, PostageCents: int(at(postage)),
			HandlingDays: days(at(handling)), TransitDays: days(at(transit)),
		})
	}
	return out
}

var parcel = Packing{Package: Package{Name: "Mailer", Class: ClassParcel}, WeightOz: 10, ContentsOz: 8}
var letter = Packing{Package: Package{Name: "Envelope", Class: ClassLetter}, WeightOz: 1.3, ContentsOz: 1}

func TestEstimateUsesTheCountry(t *testing.T) {
	var h []PastShipment
	h = append(h, past(50, "GB", ClassParcel, 8, [2]float64{1, 1}, [2]float64{6, 10}, [2]float64{600, 700})...)
	h = append(h, past(50, "IN", ClassParcel, 8, [2]float64{1, 1}, [2]float64{15, 25}, [2]float64{800, 900})...)
	e := EstimateShipping(h, "GB", parcel, now)
	if e.Service != "Asendia" || e.TransitBasis != "GB" || e.PostageBasis != "GB" {
		t.Fatalf("got %+v", e)
	}
	// p20..p80 of 6..10 days is 6.8..9.2, rounded out to 6..10.
	if e.TransitDays != [2]int{6, 10} {
		t.Fatalf("transit = %v", e.TransitDays)
	}
	if e.HandlingDays != [2]int{1, 1} {
		t.Fatalf("handling = %v", e.HandlingDays)
	}
	// now is 2026-09-29: + 1 handling + 6..10 transit.
	if e.ArriveFrom != "2026-10-06" || e.ArriveBy != "2026-10-10" {
		t.Fatalf("arrive %s..%s", e.ArriveFrom, e.ArriveBy)
	}
	if e.PostageCents[0] < 600 || e.PostageCents[1] > 700 {
		t.Fatalf("postage = %v", e.PostageCents)
	}
}

func TestEstimateFallsBackToAllInternational(t *testing.T) {
	var h []PastShipment
	h = append(h, past(50, "GB", ClassParcel, 8, [2]float64{1, 1}, [2]float64{6, 10}, [2]float64{600, 700})...)
	h = append(h, past(3, "NZ", ClassParcel, 8, [2]float64{1, 1}, [2]float64{30, 30}, [2]float64{900, 900})...)
	e := EstimateShipping(h, "NZ", parcel, now)
	if e.TransitBasis != "international" || e.TransitSamples != 53 {
		t.Fatalf("got %+v", e)
	}
}

func TestEstimatePostageMatchesWeight(t *testing.T) {
	var h []PastShipment
	h = append(h, past(30, "CA", ClassParcel, 8, [2]float64{1, 1}, [2]float64{7, 9}, [2]float64{700, 800})...)
	h = append(h, past(30, "CA", ClassParcel, 40, [2]float64{1, 1}, [2]float64{7, 9}, [2]float64{2400, 2600})...)
	heavy := parcel
	heavy.WeightOz, heavy.ContentsOz = 44, 42
	if e := EstimateShipping(h, "CA", heavy, now); e.PostageCents[0] < 2400 {
		t.Fatalf("heavy postage = %v", e.PostageCents)
	}
	if e := EstimateShipping(h, "CA", parcel, now); e.PostageCents[1] > 800 {
		t.Fatalf("light postage = %v", e.PostageCents)
	}
}

func TestEstimateServices(t *testing.T) {
	h := past(20, "US", ClassLetter, 1, [2]float64{0, 2}, [2]float64{3, 6}, [2]float64{70, 80})
	h = append(h, past(20, "US", ClassParcel, 8, [2]float64{0, 2}, [2]float64{3, 6}, [2]float64{500, 600})...)
	for _, c := range []struct {
		country string
		p       Packing
		want    string
	}{
		{"US", letter, "USPS First Class Mail"},
		{"US", parcel, "USPS Ground Advantage or UPS"},
		{"DE", letter, "USPS First Class Mail International"},
		{"DE", parcel, "Asendia"},
	} {
		if e := EstimateShipping(h, c.country, c.p, now); e.Service != c.want {
			t.Errorf("%s %s: service = %q, want %q", c.country, c.p.Package.Class, e.Service, c.want)
		}
	}
}

func TestEstimateWithoutHistory(t *testing.T) {
	e := EstimateShipping(nil, "US", parcel, now)
	if e.ArriveBy != "" || e.PostageCents != [2]int{} {
		t.Fatalf("no history should mean no estimate, got %+v", e)
	}
}

func TestEstimateFromMailed(t *testing.T) {
	h := past(30, "US", ClassParcel, 8, [2]float64{1, 1}, [2]float64{3, 5}, [2]float64{500, 600})
	mailed := time.Date(2026, 9, 20, 15, 0, 0, 0, time.UTC)
	e := EstimateShipping(h, "US", parcel, now).From(mailed)
	if e.ArriveFrom != "2026-09-23" || e.ArriveBy != "2026-09-25" {
		t.Fatalf("arrive %s..%s", e.ArriveFrom, e.ArriveBy)
	}
}

// The warehouse logs each shipment's weight from per-SKU nominal weights, without
// the packaging, so postage is matched on what's inside, not the packed weight.
func TestEstimatePostageIgnoresPackaging(t *testing.T) {
	var h []PastShipment
	h = append(h, past(30, "CA", ClassParcel, 8, [2]float64{1, 1}, [2]float64{7, 9}, [2]float64{700, 800})...)
	h = append(h, past(30, "CA", ClassParcel, 14, [2]float64{1, 1}, [2]float64{7, 9}, [2]float64{1200, 1300})...)
	boxed := Packing{Package: Package{Name: "Box", Class: ClassParcel, TareOz: 8}, ContentsOz: 8, WeightOz: 16}
	if e := EstimateShipping(h, "CA", boxed, now); e.PostageCents[1] > 800 {
		t.Fatalf("postage = %v, want the 8 oz shipments' (700-800)", e.PostageCents)
	}
}
