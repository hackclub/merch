package swag

import (
	"math"
	"slices"
	"time"
)

// PastShipment is one package mailed before (internal/store's shipping_history,
// loaded from the warehouse's records with cmd/shiphistory).
type PastShipment struct {
	Country      string
	Class        string
	WeightOz     float64  // 0 = unknown
	PostageCents int      // 0 = unknown
	HandlingDays *float64 // ordered -> mailed
	TransitDays  *float64 // mailed -> delivered
}

// Estimate is when a package should arrive and what its postage should cost,
// read off how similar packages went before. Ranges run from the 20th to the
// 80th percentile, so most packages land inside them. Zero values mean there
// was no history to go on.
type Estimate struct {
	Package      string  `json:"package"`
	WeightOz     float64 `json:"weight_oz"` // packed
	Class        string  `json:"class"`
	Service      string  `json:"service"`
	HandlingDays [2]int  `json:"handling_days"` // request (or payment) -> mailed
	TransitDays  [2]int  `json:"transit_days"`  // mailed -> delivered
	ArriveFrom   string  `json:"arrive_from"`   // YYYY-MM-DD
	ArriveBy     string  `json:"arrive_by"`
	PostageCents [2]int  `json:"postage_cents"`
	// What each figure is based on: a country code, "US" or "international",
	// and how many past shipments that was.
	TransitBasis    string `json:"transit_basis"`
	TransitSamples  int    `json:"transit_samples"`
	PostageBasis    string `json:"postage_basis"`
	PostageSamples  int    `json:"postage_samples"`
	HandlingSamples int    `json:"handling_samples"`
}

// MinSamples is the fewest past shipments a figure is worked out from; with
// fewer, the estimate widens to all of the US or all international shipments.
const MinSamples = 10

// Dates are in Hack Club HQ's time zone, where the warehouse ships from.
var shipZone = func() *time.Location {
	if l, err := time.LoadLocation("America/New_York"); err == nil {
		return l
	}
	return time.UTC
}()

// Services names who carries each class of package, by destination.
func Service(class, country string) string {
	intl := IsInternational(country)
	switch {
	case class == ClassLetter && intl:
		return "USPS First Class Mail International"
	case class == ClassLetter:
		return "USPS First Class Mail"
	case intl:
		return "Asendia"
	default:
		return "USPS Ground Advantage or UPS"
	}
}

// EstimateShipping estimates a packed request going to country, ordered now.
// An empty country means anywhere outside the US.
func EstimateShipping(history []PastShipment, country string, p Packing, now time.Time) Estimate {
	intl := IsInternational(country)
	scope := "US"
	if intl {
		scope = "international"
	}
	class := p.Package.Class
	e := Estimate{Package: p.Package.Name, WeightOz: p.WeightOz, Class: class, Service: Service(class, country)}

	inScope := func(s PastShipment) bool { return IsInternational(s.Country) == intl }
	// pick returns the first filter with enough samples of f, and its basis.
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

	// Postage depends on weight, so compare with packages of about the same
	// weight. The warehouse logs weights from per-SKU weights without the
	// packaging (78 single M shirts all logged at exactly 6.0 oz), and labels
	// were bought at those weights, so match on the contents.
	near := func(s PastShipment) bool {
		return s.WeightOz > 0 && math.Abs(s.WeightOz-p.ContentsOz) <= max(2, 0.25*p.ContentsOz)
	}
	postage, basis := pick(func(s PastShipment) (float64, bool) {
		return float64(s.PostageCents), s.PostageCents > 0
	}, level{country, func(s PastShipment) bool { return sameCountry(s) && near(s) }},
		level{scope, func(s PastShipment) bool { return sameScope(s) && near(s) }})
	if postage != nil {
		e.PostageCents = [2]int{int(math.Round(percentile(postage, 0.2))), int(math.Round(percentile(postage, 0.8)))}
		e.PostageBasis, e.PostageSamples = basis, len(postage)
	}

	// How long the warehouse takes to mail an order.
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

// From re-dates an estimate for a package already mailed at mailed.
func (e Estimate) From(mailed time.Time) Estimate {
	e.HandlingDays = [2]int{}
	if e.TransitSamples > 0 {
		m := mailed.In(shipZone)
		e.ArriveFrom = m.AddDate(0, 0, e.TransitDays[0]).Format(time.DateOnly)
		e.ArriveBy = m.AddDate(0, 0, e.TransitDays[1]).Format(time.DateOnly)
	}
	return e
}

// window is the 20th to 80th percentile of xs in whole days, rounded outward.
func window(xs []float64) [2]int {
	return [2]int{int(math.Floor(percentile(xs, 0.2))), int(math.Ceil(percentile(xs, 0.8)))}
}

// percentile interpolates linearly, like Postgres's percentile_cont.
func percentile(xs []float64, q float64) float64 {
	s := slices.Clone(xs)
	slices.Sort(s)
	pos := q * float64(len(s)-1)
	lo := int(math.Floor(pos))
	if lo+1 >= len(s) {
		return s[len(s)-1]
	}
	return s[lo] + (pos-float64(lo))*(s[lo+1]-s[lo])
}
