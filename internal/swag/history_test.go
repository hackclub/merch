package swag

import (
	"math"
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"
	"time"
)

// scanEstimate is EstimateShipping as it was before History: a scan of every
// past shipment per figure. History must give the same answers.
func scanEstimate(history []PastShipment, country string, p Packing, now time.Time) Estimate {
	intl := IsInternational(country)
	scope := "US"
	if intl {
		scope = "international"
	}
	class := p.Package.Class
	e := Estimate{Package: p.Package.Name, WeightOz: p.WeightOz, Class: class, Service: Service(class, country)}
	inScope := func(s PastShipment) bool { return IsInternational(s.Country) == intl }
	type level struct {
		basis string
		keep  func(PastShipment) bool
	}
	pick := func(f func(PastShipment) (float64, bool), levels ...level) ([]float64, string) {
		for _, l := range levels {
			var xs []float64
			for _, s := range history {
				if !l.keep(s) {
					continue
				}
				if x, ok := f(s); ok {
					xs = append(xs, x)
				}
			}
			if len(xs) >= MinSamples {
				slices.Sort(xs)
				return xs, l.basis
			}
		}
		return nil, ""
	}
	sameCountry := func(s PastShipment) bool { return s.Country == country && s.Class == class }
	sameScope := func(s PastShipment) bool { return inScope(s) && s.Class == class }
	transit, basis := pick(func(s PastShipment) (float64, bool) {
		if s.TransitDays == nil {
			return 0, false
		}
		return *s.TransitDays, true
	}, level{country, sameCountry}, level{scope, sameScope}, level{scope, inScope})
	if transit != nil {
		e.TransitDays = window(transit)
		e.TransitBasis, e.TransitSamples = basis, len(transit)
	}
	near := func(s PastShipment) bool {
		return s.WeightOz > 0 && math.Abs(s.WeightOz-p.ContentsOz) <= max(2, 0.25*p.ContentsOz)
	}
	postage, basis := pick(func(s PastShipment) (float64, bool) {
		return float64(s.PostageCents), s.PostageCents > 0
	}, level{country, func(s PastShipment) bool { return sameCountry(s) && near(s) }},
		level{scope, func(s PastShipment) bool { return sameScope(s) && near(s) }})
	if postage != nil {
		e.PostageCents = [2]int{int(math.Round(percentile(postage, 0.2))), int(math.Round(percentile(postage, 0.8)))}
		e.PostageTypicalCents = int(math.Round(percentile(postage, 0.5)))
		e.PostageBasis, e.PostageSamples = basis, len(postage)
	}
	handling, _ := pick(func(s PastShipment) (float64, bool) {
		if s.HandlingDays == nil {
			return 0, false
		}
		return *s.HandlingDays, true
	}, level{scope, sameScope}, level{scope, inScope})
	if handling != nil {
		e.HandlingDays = window(handling)
		e.HandlingSamples = len(handling)
	}
	if transit != nil {
		start := now.In(shipZone)
		e.ArriveFrom = start.AddDate(0, 0, e.HandlingDays[0]+e.TransitDays[0]).Format(time.DateOnly)
		e.ArriveBy = start.AddDate(0, 0, e.HandlingDays[1]+e.TransitDays[1]).Format(time.DateOnly)
	}
	return e
}

// randomHistory is n made-up shipments, with gaps (unknown weight, postage,
// handling or transit) like the real history has.
func randomHistory(r *rand.Rand, n int) []PastShipment {
	countries := []string{"US", "US", "US", "CA", "GB", "IN", "DE", "NZ", ""}
	classes := []string{ClassParcel, ClassLetter}
	var h []PastShipment
	for range n {
		s := PastShipment{Country: countries[r.IntN(len(countries))], Class: classes[r.IntN(2)]}
		if r.IntN(5) > 0 {
			s.WeightOz = math.Round(r.Float64()*400) / 10
		}
		if r.IntN(4) > 0 {
			s.PostageCents = 100 + r.IntN(3000)
		}
		if r.IntN(3) > 0 {
			s.HandlingDays = days(float64(r.IntN(6)))
		}
		if r.IntN(3) > 0 {
			s.TransitDays = days(float64(1 + r.IntN(20)))
		}
		h = append(h, s)
	}
	return h
}

func TestHistoryMatchesAScan(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	now := time.Date(2026, 10, 16, 15, 0, 0, 0, time.UTC)
	for _, n := range []int{0, 5, 40, 400, 3000} {
		h := randomHistory(r, n)
		idx := NewHistory(h)
		for _, country := range []string{"US", "CA", "GB", "NZ", "FR", "", "us"} {
			for _, class := range []string{ClassParcel, ClassLetter} {
				for _, oz := range []float64{0, 0.5, 1, 2, 6, 8, 8.4, 16, 30, 48} {
					p := Packing{Package: Package{Name: "x", Class: class}, ContentsOz: oz, WeightOz: oz + 1}
					want, got := scanEstimate(h, country, p, now), idx.Estimate(country, p, now)
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("n=%d %q %s %v oz:\n got %+v\nwant %+v", n, country, class, oz, got, want)
					}
				}
			}
		}
	}
}

// BenchmarkHistoryEstimate is one estimate against a history the size of
// production's (47k shipments).
func BenchmarkHistoryEstimate(b *testing.B) {
	h := NewHistory(randomHistory(rand.New(rand.NewPCG(3, 4)), 47000))
	now := time.Now()
	for i := 0; b.Loop(); i++ {
		h.Estimate([]string{"US", "CA", ""}[i%3], Packing{Package: Package{Class: ClassParcel}, ContentsOz: 8}, now)
	}
}

func BenchmarkScanEstimate(b *testing.B) {
	h := randomHistory(rand.New(rand.NewPCG(3, 4)), 47000)
	now := time.Now()
	for i := 0; b.Loop(); i++ {
		scanEstimate(h, []string{"US", "CA", ""}[i%3], Packing{Package: Package{Class: ClassParcel}, ContentsOz: 8}, now)
	}
}
