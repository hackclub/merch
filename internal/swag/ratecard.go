package swag

import (
	"cmp"
	"math"
	"slices"
	"time"
)

// International postage comes off rate cards read from past shipments.
//
// Asendia and USPS price mail abroad by destination and whole ounce: a base
// plus so much an ounce, rounded up to the ounce (Canada, 2026: $8.90 +
// $0.414/oz; India $6.51 + $0.508/oz), and a letter is $1.93 up to 1 oz and
// $3.85 up to 2 oz. Prices are the same for every package of that weight to
// that country, to the cent, until rates go up (Asendia: +5.3% in January
// 2025, +15% in January 2026; USPS each July). So instead of averaging
// packages of about the same weight, an estimate reads the country's card at
// the package's whole-ounce weight:
//
//  1. A price index per class, chained month to month from the weights and
//     countries shipped in both a month and the six months before it, brings
//     every past price to today's rates.
//  2. Each country's card has, for each whole ounce shipped there, the prices
//     from the last CardDays that weight was shipped.
//  3. A price far off the country's line (a parcel that went another way) is
//     dropped; weights in between read off the line between their
//     neighbours, and heavier or lighter ones follow the country's rate per
//     ounce.
//  4. A country with fewer than MinCardSamples past packages goes by packages
//     of the same weight to anywhere abroad.
//
// Backtested on the warehouse's 2025-2026 Asendia parcels (estimating each
// month from the history before it), 84-93% of estimates were within 25¢ of
// the label's price and 93-97% within $1; averaging by weight was within 25¢
// for 2-29% of them.

// CardDays is how far back from a weight's newest price its card looks.
const CardDays = 90

// MinCardSamples is the fewest past packages to a country that make its own
// rate card.
const MinCardSamples = 3

// cardMaxOz leaves out the odd heavier parcel: international packages stop at
// 3 lb (MaxInternationalOz), and Asendia's heavier ones go other ways.
const cardMaxOz = 64

type cardKey struct{ country, class string }

// cardPrice is what one whole-ounce weight costs: the 20th, 50th and 80th
// percentile of its recent prices, and how many there were.
type cardPrice struct {
	oz            int
	p20, p50, p80 float64
	n             int
}

type rateCard struct {
	prices []cardPrice // by weight
	perOz  float64     // the country's rate per ounce
	sloped bool        // perOz comes from weights at least 3 oz apart
}

type cardShipment struct {
	country string
	oz      int
	cents   float64
	month   int
	on      time.Time
}

type rateCards struct {
	cards map[cardKey]*rateCard
	// For countries without a card: every package of the class abroad, at
	// today's rates, by weight.
	all    map[string][]cardShipment
	newest map[string]time.Time
}

func wholeOz(oz float64) int { return int(math.Ceil(oz - 1e-9)) }

func monthOf(t time.Time) int { return t.Year()*12 + int(t.Month()) - 1 }

func median(xs []float64) float64 {
	s := slices.Clone(xs)
	slices.Sort(s)
	return percentile(s, 0.5)
}

// newRateCards reads the rate cards out of past shipments abroad.
func newRateCards(ships []PastShipment) *rateCards {
	byClass := map[string][]cardShipment{}
	for _, s := range ships {
		if !IsInternational(s.Country) || s.WeightOz <= 0 || s.WeightOz > cardMaxOz || s.PostageCents <= 0 {
			continue
		}
		byClass[s.Class] = append(byClass[s.Class], cardShipment{country: s.Country, oz: wholeOz(s.WeightOz),
			cents: float64(s.PostageCents), month: monthOf(s.ShippedOn), on: s.ShippedOn})
	}
	rc := &rateCards{cards: map[cardKey]*rateCard{}, all: map[string][]cardShipment{}, newest: map[string]time.Time{}}
	for class, ss := range byClass {
		atToday(ss)
		rc.all[class] = ss
		byCountry := map[string][]cardShipment{}
		for _, s := range ss {
			byCountry[s.country] = append(byCountry[s.country], s)
			if s.on.After(rc.newest[class]) {
				rc.newest[class] = s.on
			}
		}
		for country, cs := range byCountry {
			if len(cs) >= MinCardSamples {
				rc.cards[cardKey{country, class}] = newRateCard(cs)
			}
		}
	}
	return rc
}

// atToday brings every price to today's rates with a price index chained
// month to month: each month against the six months with shipments before
// it, over the country and weight pairs both have (at least 3; otherwise
// prices held).
func atToday(ss []cardShipment) {
	type bucket struct {
		country string
		oz      int
	}
	byMonth := map[int][]cardShipment{}
	for _, s := range ss {
		byMonth[s.month] = append(byMonth[s.month], s)
	}
	months := make([]int, 0, len(byMonth))
	for m := range byMonth {
		months = append(months, m)
	}
	slices.Sort(months)
	level := map[int]float64{}
	medians := func(ss []cardShipment, price func(cardShipment) float64) map[bucket]float64 {
		xs := map[bucket][]float64{}
		for _, s := range ss {
			b := bucket{s.country, s.oz}
			xs[b] = append(xs[b], price(s))
		}
		out := map[bucket]float64{}
		for b, x := range xs {
			out[b] = median(x)
		}
		return out
	}
	for k, m := range months {
		if k == 0 {
			level[m] = 1
			continue
		}
		prev := months[k-1]
		var before []cardShipment
		for _, pm := range months[max(0, k-6):k] {
			before = append(before, byMonth[pm]...)
		}
		base := medians(before, func(s cardShipment) float64 { return s.cents * level[prev] / level[s.month] })
		var ratios []float64
		for b, x := range medians(byMonth[m], func(s cardShipment) float64 { return s.cents }) {
			if y, ok := base[b]; ok {
				ratios = append(ratios, x/y)
			}
		}
		level[m] = level[prev]
		if len(ratios) >= 3 {
			level[m] *= median(ratios)
		}
	}
	if len(months) == 0 {
		return
	}
	today := level[months[len(months)-1]]
	for i := range ss {
		ss[i].cents *= today / level[ss[i].month]
	}
}

// newRateCard is one country's card from its packages, at today's rates.
func newRateCard(cs []cardShipment) *rateCard {
	byOz := map[int][]cardShipment{}
	for _, s := range cs {
		byOz[s.oz] = append(byOz[s.oz], s)
	}
	var prices []cardPrice
	for oz, ss := range byOz {
		var newest time.Time
		for _, s := range ss {
			if s.on.After(newest) {
				newest = s.on
			}
		}
		var xs []float64
		for _, s := range ss {
			if !s.on.Before(newest.AddDate(0, 0, -CardDays)) {
				xs = append(xs, s.cents)
			}
		}
		slices.Sort(xs)
		prices = append(prices, cardPrice{oz: oz, p20: percentile(xs, 0.2), p50: percentile(xs, 0.5), p80: percentile(xs, 0.8), n: len(xs)})
	}
	slices.SortFunc(prices, func(a, b cardPrice) int { return cmp.Compare(a.oz, b.oz) })
	c := &rateCard{prices: prices}
	c.perOz, c.sloped = c.slope()
	if len(prices) >= 3 {
		// Drop prices far off the line (more than 25% or $1.50).
		base := median(mapPrices(prices, func(p cardPrice) float64 { return p.p50 - c.perOz*float64(p.oz) }))
		c.prices = slices.DeleteFunc(c.prices, func(p cardPrice) bool {
			fit := base + c.perOz*float64(p.oz)
			return math.Abs(p.p50-fit) > max(0.25*fit, 150)
		})
		c.perOz, c.sloped = c.slope()
	}
	return c
}

func mapPrices(ps []cardPrice, f func(cardPrice) float64) []float64 {
	out := make([]float64, len(ps))
	for i, p := range ps {
		out[i] = f(p)
	}
	return out
}

// slope is the card's rate per ounce: the median slope between any two of its
// weights (Theil-Sen), which one odd price can't pull.
func (c *rateCard) slope() (float64, bool) {
	ps := c.prices
	if len(ps) < 2 {
		return 0, false
	}
	var slopes []float64
	for i := range ps {
		for j := i + 1; j < len(ps); j++ {
			slopes = append(slopes, (ps[j].p50-ps[i].p50)/float64(ps[j].oz-ps[i].oz))
		}
	}
	return median(slopes), ps[len(ps)-1].oz-ps[0].oz >= 3
}

// classPerOz is the typical rate per ounce across countries' cards, for
// cards without their own.
func (rc *rateCards) classPerOz(class string) float64 {
	var xs []float64
	for k, c := range rc.cards {
		if k.class == class && c.sloped {
			xs = append(xs, c.perOz)
		}
	}
	if len(xs) == 0 {
		return 0
	}
	return median(xs)
}

// estimate is the postage for a package of oz to country abroad: p20, p50 and
// p80, the basis (the country or "international") and how many past packages
// it rests on. n is 0 without history.
func (rc *rateCards) estimate(country, class string, oz float64) (p [3]float64, basis string, n int) {
	w := wholeOz(oz)
	if c := rc.cards[cardKey{country, class}]; c != nil && len(c.prices) > 0 {
		p, n = c.at(w, rc.classPerOz(class))
		return p, country, n
	}
	// No card: packages of about the same weight (within 15%, at least an
	// ounce) to anywhere abroad, recent ones first.
	d := max(1, int(math.Round(0.15*float64(w))))
	near := func(recent bool) []float64 {
		var xs []float64
		for _, s := range rc.all[class] {
			if abs(s.oz-w) <= d && (!recent || !s.on.Before(rc.newest[class].AddDate(0, 0, -RecentPostageDays))) {
				xs = append(xs, s.cents)
			}
		}
		return xs
	}
	xs := near(true)
	if len(xs) < MinSamples {
		xs = near(false)
	}
	if len(xs) == 0 {
		return p, "", 0
	}
	slices.Sort(xs)
	return [3]float64{percentile(xs, 0.2), percentile(xs, 0.5), percentile(xs, 0.8)}, "international", len(xs)
}

func abs(x int) int { return max(x, -x) }

// at reads the card at w whole ounces.
func (c *rateCard) at(w int, classPerOz float64) ([3]float64, int) {
	ps := c.prices
	three := func(p cardPrice, plus float64) [3]float64 {
		return [3]float64{p.p20 + plus, p.p50 + plus, p.p80 + plus}
	}
	i, found := slices.BinarySearchFunc(ps, w, func(p cardPrice, w int) int { return cmp.Compare(p.oz, w) })
	switch {
	case found:
		return three(ps[i], 0), ps[i].n
	case i > 0 && i < len(ps):
		lo, hi := ps[i-1], ps[i]
		f := float64(w-lo.oz) / float64(hi.oz-lo.oz)
		mix := func(a, b float64) float64 { return a + f*(b-a) }
		return [3]float64{mix(lo.p20, hi.p20), mix(lo.p50, hi.p50), mix(lo.p80, hi.p80)}, lo.n + hi.n
	}
	perOz := classPerOz
	if c.sloped {
		perOz = c.perOz
	}
	nearest := ps[0]
	if i == len(ps) {
		nearest = ps[len(ps)-1]
	}
	if nearest.p50+perOz*float64(w-nearest.oz) <= 0 {
		perOz = 0
	}
	return three(nearest, perOz*float64(w-nearest.oz)), nearest.n
}
