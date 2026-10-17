package swag

import (
	"cmp"
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
	HandlingDays *float64 // ordered -> mailed, in business days
	TransitDays  *float64 // mailed -> delivered, in business days
	ShippedOn    time.Time
}

// BusinessDays counts the weekdays after from, up to and including to.
func BusinessDays(from, to time.Time) int {
	n := 0
	for d := from.AddDate(0, 0, 1); !d.After(to); d = d.AddDate(0, 0, 1) {
		if wd := d.Weekday(); wd != time.Saturday && wd != time.Sunday {
			n++
		}
	}
	return n
}

// AddBusinessDays is the nth weekday after t (t itself when n is 0).
func AddBusinessDays(t time.Time, n int) time.Time {
	for n > 0 {
		t = t.AddDate(0, 0, 1)
		if wd := t.Weekday(); wd != time.Saturday && wd != time.Sunday {
			n--
		}
	}
	return t
}

// Estimate is when a package should arrive and what its postage should cost,
// read off how similar packages went before. Days are business days (Mon–Fri):
// the warehouse doesn't ship on weekends. Ranges run from the 20th to the 80th
// percentile, so most packages land inside them. Zero values mean there was no
// history to go on.
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
	// PostageTypicalCents is the median postage; international requests pay it.
	PostageTypicalCents int `json:"postage_typical_cents"`
	// FeeCents is what the requester pays for shipping (see InitialStatus).
	FeeCents int `json:"fee_cents"`
	// What each figure is based on: a country code, "US" or "international",
	// and how many past shipments that was.
	TransitBasis    string `json:"transit_basis"`
	TransitSamples  int    `json:"transit_samples"`
	PostageBasis    string `json:"postage_basis"`
	PostageSamples  int    `json:"postage_samples"`
	HandlingSamples int    `json:"handling_samples"`
	// Restock is set when an item is backordered: the request ships once the
	// restock reaches the warehouse, on no known date, so ArriveFrom and
	// ArriveBy are empty and the days count from the restock.
	Restock bool `json:"restock"`
}

// Backordered says whether qty of a SKU waits on a restock: the warehouse
// doesn't have enough (inStock goes below zero when orders are already
// waiting) but has more on order.
func Backordered(qty, inStock, inbound int) bool {
	return inStock < qty && inbound > 0
}

// AfterRestock re-counts an estimate from when a restock reaches the
// warehouse, which takes it a day or two to receive and shelve.
func (e Estimate) AfterRestock() Estimate {
	e.Restock = true
	e.HandlingDays = [2]int{e.HandlingDays[0] + 1, e.HandlingDays[1] + 2}
	e.ArriveFrom, e.ArriveBy = "", ""
	return e
}

// MinSamples is the fewest past shipments a figure is worked out from; with
// fewer, the estimate widens to all of the US or all international shipments.
const MinSamples = 10

// RecentPostageDays is how far back US postage looks first, counted from the
// newest shipment in the history. Rates go up every year (a US parcel of 5-9 oz
// cost $4.71 in early 2025 and $7.27 in mid-2026), so older prices come in only
// when there are too few recent ones.
const RecentPostageDays = 180

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
// An empty country means anywhere outside the US. To make many estimates from
// one history, index it once with NewHistory.
func EstimateShipping(history []PastShipment, country string, p Packing, now time.Time) Estimate {
	return NewHistory(history).Estimate(country, p, now)
}

// History is past shipments indexed for estimates: each figure's known values,
// sorted, for every group an estimate can widen to (same country and class,
// same side of the border and class, or that side of the border).
type History struct {
	transit, handling map[group][]float64
	postage           map[group][]weighed // sorted by weight; US only
	cards             *rateCards          // postage abroad
}

type group struct {
	byCountry bool
	country   string // byCountry
	intl      bool   // !byCountry
	anyClass  bool
	class     string
	recent    bool // postage only: shipped in the last RecentPostageDays
}

type weighed struct{ oz, cents float64 }

func countryGroup(country, class string) group {
	return group{byCountry: true, country: country, class: class}
}

func scopeGroup(intl bool, class string) group { return group{intl: intl, class: class} }

func anyClassGroup(intl bool) group { return group{intl: intl, anyClass: true} }

func recentGroup(g group) group { g.recent = true; return g }

// NewHistory indexes past shipments for estimates.
func NewHistory(ships []PastShipment) *History {
	h := &History{transit: map[group][]float64{}, handling: map[group][]float64{}, postage: map[group][]weighed{}}
	var newest time.Time
	for _, s := range ships {
		if s.ShippedOn.After(newest) {
			newest = s.ShippedOn
		}
	}
	since := newest.AddDate(0, 0, -RecentPostageDays)
	for _, s := range ships {
		intl := IsInternational(s.Country)
		if s.TransitDays != nil {
			for _, g := range []group{countryGroup(s.Country, s.Class), scopeGroup(intl, s.Class), anyClassGroup(intl)} {
				h.transit[g] = append(h.transit[g], *s.TransitDays)
			}
		}
		if s.HandlingDays != nil {
			for _, g := range []group{scopeGroup(intl, s.Class), anyClassGroup(intl)} {
				h.handling[g] = append(h.handling[g], *s.HandlingDays)
			}
		}
		if s.WeightOz > 0 && s.PostageCents > 0 {
			w := weighed{s.WeightOz, float64(s.PostageCents)}
			for _, g := range []group{countryGroup(s.Country, s.Class), scopeGroup(intl, s.Class)} {
				h.postage[g] = append(h.postage[g], w)
				if !s.ShippedOn.Before(since) {
					g.recent = true
					h.postage[g] = append(h.postage[g], w)
				}
			}
		}
	}
	for _, xs := range h.transit {
		slices.Sort(xs)
	}
	for _, xs := range h.handling {
		slices.Sort(xs)
	}
	for _, ws := range h.postage {
		slices.SortFunc(ws, func(a, b weighed) int { return cmp.Compare(a.oz, b.oz) })
	}
	h.cards = newRateCards(ships)
	return h
}

// Estimate estimates a packed request going to country, ordered now. An empty
// country means anywhere outside the US.
func (h *History) Estimate(country string, p Packing, now time.Time) Estimate {
	intl := IsInternational(country)
	scope := "US"
	if intl {
		scope = "international"
	}
	class := p.Package.Class
	e := Estimate{Package: p.Package.Name, WeightOz: p.WeightOz, Class: class, Service: Service(class, country)}

	// first returns the first group with enough samples, and its basis.
	first := func(m map[group][]float64, groups []group, bases []string) ([]float64, string) {
		for i, g := range groups {
			if xs := m[g]; len(xs) >= MinSamples {
				return xs, bases[i]
			}
		}
		return nil, ""
	}

	transit, basis := first(h.transit,
		[]group{countryGroup(country, class), scopeGroup(intl, class), anyClassGroup(intl)},
		[]string{country, scope, scope})
	if transit != nil {
		e.TransitDays = window(transit)
		e.TransitBasis, e.TransitSamples = basis, len(transit)
	}

	if intl {
		// Abroad, postage comes off the country's rate card (ratecard.go).
		if p, basis, n := h.cards.estimate(country, class, p.WeightOz); n > 0 {
			e.PostageCents = [2]int{int(math.Round(p[0])), int(math.Round(p[2]))}
			e.PostageTypicalCents = int(math.Round(p[1]))
			e.PostageBasis, e.PostageSamples = basis, n
		}
	} else {
		e.estimateUSPostage(h, country, p, class)
	}

	// How long the warehouse takes to mail an order.
	handling, _ := first(h.handling, []group{scopeGroup(intl, class), anyClassGroup(intl)}, []string{scope, scope})
	if handling != nil {
		e.HandlingDays = window(handling)
		e.HandlingSamples = len(handling)
	}

	if transit != nil {
		start := now.In(shipZone)
		e.ArriveFrom = AddBusinessDays(start, e.HandlingDays[0]+e.TransitDays[0]).Format(time.DateOnly)
		e.ArriveBy = AddBusinessDays(start, e.HandlingDays[1]+e.TransitDays[1]).Format(time.DateOnly)
	}
	return e
}

// estimateUSPostage sets US postage from packages of about the same packed
// weight. US parcels are priced by zone (how far the ZIP is) as well as
// weight, and the zone isn't known, so this takes the middle of what packages
// like it cost.
func (e *Estimate) estimateUSPostage(h *History, country string, p Packing, class string) {
	intl, scope := false, "US"
	// The warehouse weighs packages with their packaging (one poster logs
	// 7.2 oz: 1.2 oz of paper in a 6 oz StayFlat) and buys labels at that
	// weight, so match on the packed weight. Recent prices come first (see
	// RecentPostageDays).
	near := func(ws []weighed) []float64 {
		d := max(2, 0.25*p.WeightOz)
		i, _ := slices.BinarySearchFunc(ws, p.WeightOz-d-1e-6, func(w weighed, oz float64) int { return cmp.Compare(w.oz, oz) })
		var xs []float64
		for ; i < len(ws) && ws[i].oz <= p.WeightOz+d+1e-6; i++ {
			if math.Abs(ws[i].oz-p.WeightOz) <= d {
				xs = append(xs, ws[i].cents)
			}
		}
		return xs
	}
	for _, l := range []struct {
		g     group
		basis string
	}{{recentGroup(countryGroup(country, class)), country}, {countryGroup(country, class), country},
		{recentGroup(scopeGroup(intl, class)), scope}, {scopeGroup(intl, class), scope}} {
		postage := near(h.postage[l.g])
		if len(postage) < MinSamples {
			continue
		}
		slices.Sort(postage)
		e.PostageCents = [2]int{int(math.Round(percentile(postage, 0.2))), int(math.Round(percentile(postage, 0.8)))}
		e.PostageTypicalCents = int(math.Round(percentile(postage, 0.5)))
		e.PostageBasis, e.PostageSamples = l.basis, len(postage)
		break
	}
}

// From re-dates an estimate for a package already mailed at mailed.
func (e Estimate) From(mailed time.Time) Estimate {
	e.HandlingDays = [2]int{}
	if e.TransitSamples > 0 {
		m := mailed.In(shipZone)
		e.ArriveFrom = AddBusinessDays(m, e.TransitDays[0]).Format(time.DateOnly)
		e.ArriveBy = AddBusinessDays(m, e.TransitDays[1]).Format(time.DateOnly)
	}
	return e
}

// window is the 20th to 80th percentile of sorted xs in whole days, rounded
// outward.
func window(xs []float64) [2]int {
	return [2]int{int(math.Floor(percentile(xs, 0.2))), int(math.Ceil(percentile(xs, 0.8)))}
}

// percentile of sorted xs interpolates linearly, like Postgres's
// percentile_cont.
func percentile(xs []float64, q float64) float64 {
	pos := q * float64(len(xs)-1)
	lo := int(math.Floor(pos))
	if lo+1 >= len(xs) {
		return xs[len(xs)-1]
	}
	return xs[lo] + (pos-float64(lo))*(xs[lo+1]-xs[lo])
}
