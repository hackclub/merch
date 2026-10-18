package swag

import (
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"
)

// TestBacktestPostage estimates each month's real labels from the history
// before that month and reports how close the estimates came. It needs a
// shipping history export (cmd/shiphistory's CSV; keep it out of the repo):
//
//	MERCH_BACKTEST_CSV=~/merch-ship/history.csv go test ./internal/swag -run Backtest -v
//
// BT_LIST=<a row's name> (e.g. "card, past heaviest +13 oz+" or "up to 40 lb")
// lists that row's labels one by one.
func TestBacktestPostage(t *testing.T) {
	path := os.Getenv("MERCH_BACKTEST_CSV")
	if path == "" {
		t.Skip("set MERCH_BACKTEST_CSV to a shipping history export")
	}
	ships := readBacktestCSV(t, path)
	type tally struct {
		n, in25, in100 int
		err, abs       float64
	}
	add := func(m map[string]*tally, k string, est, label float64) {
		if m[k] == nil {
			m[k] = &tally{}
		}
		c, d := m[k], est-label
		c.n++
		c.err += d
		c.abs += math.Abs(d)
		if math.Abs(d) <= 25 {
			c.in25++
		}
		if math.Abs(d) <= 100 {
			c.in100++
		}
	}
	intl, us := map[string]*tally{}, map[string]*tally{}
	start := time.Date(2025, 4, 1, 0, 0, 0, 0, time.UTC)
	for m := start; m.Before(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)); m = m.AddDate(0, 1, 0) {
		var train, test []PastShipment
		for _, s := range ships {
			switch {
			case s.ShippedOn.Before(m):
				train = append(train, s)
			case s.ShippedOn.Before(m.AddDate(0, 1, 0)) && s.Class == ClassParcel && s.PostageCents > 0 && s.WeightOz > 0:
				test = append(test, s)
			}
		}
		if len(test) == 0 {
			continue
		}
		h := NewHistory(train)
		for _, s := range test {
			if IsInternational(s.Country) {
				if s.WeightOz > MaxInternationalOz {
					continue
				}
				p, _, n := h.cards.estimate(s.Country, ClassParcel, s.WeightOz)
				if n == 0 {
					continue
				}
				k := cardPath(h.cards, s.Country, s.WeightOz)
				add(intl, "all", p[1], float64(s.PostageCents))
				add(intl, k, p[1], float64(s.PostageCents))
				if os.Getenv("BT_LIST") == k {
					c := h.cards.cards[cardKey{s.Country, ClassParcel}]
					t.Logf("%s %s %.1f oz: est %.0f label %d (card %d weights, heaviest %d oz, %.0f¢/oz)", s.ShippedOn.Format(time.DateOnly), s.Country, s.WeightOz, p[1], s.PostageCents, len(c.prices), c.prices[len(c.prices)-1].oz, c.perOz)
				}
				if s.WeightOz > 16 {
					add(intl, "over 1 lb", p[1], float64(s.PostageCents))
				}
				continue
			}
			e := h.Estimate("US", Packing{Package: Package{Class: ClassParcel}, WeightOz: s.WeightOz}, s.ShippedOn)
			if e.PostageSamples == 0 {
				add(us, usBand(s.WeightOz)+" no estimate", 0, float64(s.PostageCents))
				continue
			}
			add(us, usBand(s.WeightOz), float64(e.PostageTypicalCents), float64(s.PostageCents))
			if os.Getenv("BT_LIST") == usBand(s.WeightOz) {
				t.Logf("%s %.1f lb: est %d [%v] label %d (%s, %d samples)", s.ShippedOn.Format(time.DateOnly), s.WeightOz/16, e.PostageTypicalCents, e.PostageCents, s.PostageCents, e.PostageBasis, e.PostageSamples)
			}
		}
	}
	report := func(name string, m map[string]*tally) {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		t.Logf("%s: estimate vs label, from %s", name, start.Format("2006-01"))
		for _, k := range keys {
			c := m[k]
			t.Logf("  %-28s n=%5d  within 25¢ %3.0f%%  within $1 %3.0f%%  mean error %+7.0f¢  mean |error| %5.0f¢",
				k, c.n, 100*float64(c.in25)/float64(c.n), 100*float64(c.in100)/float64(c.n), c.err/float64(c.n), c.abs/float64(c.n))
		}
	}
	report("abroad (Asendia)", intl)
	report("US", us)
}

// cardPath names how the rate card prices oz to country: off its own prices,
// between them, past its heaviest or lightest, or without a card.
func cardPath(rc *rateCards, country string, oz float64) string {
	c := rc.cards[cardKey{country, ClassParcel}]
	if c == nil || len(c.prices) == 0 {
		return "no card"
	}
	w := wholeOz(oz)
	ps := c.prices
	switch _, found := slices.BinarySearchFunc(ps, w, func(p cardPrice, w int) int { return cmp3(p.oz, w) }); {
	case found:
		return "card, that weight"
	case w > ps[len(ps)-1].oz:
		d := w - ps[len(ps)-1].oz
		switch {
		case d <= 4:
			return "card, past heaviest +1-4 oz"
		case d <= 12:
			return "card, past heaviest +5-12 oz"
		}
		return "card, past heaviest +13 oz+"
	case w < ps[0].oz:
		return "card, under lightest"
	}
	return "card, between weights"
}

func cmp3(a, b int) int { return max(-1, min(1, a-b)) }

func usBand(oz float64) string {
	for _, lb := range []float64{1, 3, 5, 10, 20, 40} {
		if oz <= lb*16 {
			return fmt.Sprintf("up to %2g lb", lb)
		}
	}
	return "over 40 lb"
}

func readBacktestCSV(t *testing.T, path string) []PastShipment {
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	var out []PastShipment
	for _, rec := range rows {
		if rec[0] == "source" {
			continue
		}
		oz, _ := strconv.ParseFloat(rec[3], 64)
		cents, _ := strconv.Atoi(rec[4])
		on, err := time.Parse(time.DateOnly, rec[6])
		if err != nil {
			t.Fatalf("%v: %v", rec, err)
		}
		out = append(out, PastShipment{Country: rec[1], Class: rec[2], WeightOz: oz, PostageCents: cents, ShippedOn: on})
	}
	return out
}
