package swag

import (
	"encoding/json"
	"math"
	"os"
	"slices"
	"testing"
	"time"
)

// TestBacktestPackedQuotes checks the complete quote, not just a rate card at
// the label's weight. Both inputs must stay outside git. JSON has items (the
// proposed catalog) and shipments with original request lines, the exact
// matching quote's weight, the fee actually charged (net of refunds), and the
// warehouse label's weight and postage. Do not join quotes by user/time alone:
// match country and the complete lines too, including SKUs and quantities.
//
// MERCH_PACKING_BACKTEST_JSON=... MERCH_BACKTEST_CSV=... go test ./internal/swag -run BacktestPacked -v
//
// Results are retrospective replays, not measured savings on future requests.
func TestBacktestPackedQuotes(t *testing.T) {
	path := os.Getenv("MERCH_PACKING_BACKTEST_JSON")
	if path == "" {
		t.Skip("set MERCH_PACKING_BACKTEST_JSON and MERCH_BACKTEST_CSV")
	}
	var input struct {
		Items     []Item `json:"items"`
		Shipments []struct {
			Country        string  `json:"country"`
			Class          string  `json:"class"`
			CreatedOn      string  `json:"created_on"`
			Lines          []Line  `json:"lines"`
			QuotedWeightOz float64 `json:"quoted_weight_oz"`
			LabelWeightOz  float64 `json:"label_weight_oz"`
			ChargedCents   int     `json:"charged_cents"`
			ActualCents    int     `json:"actual_cents"`
		} `json:"shipments"`
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &input); err != nil {
		t.Fatal(err)
	}
	catalog := map[int64]Item{}
	for _, it := range input.Items {
		catalog[it.ID] = it
	}
	ships := readBacktestCSV(t, os.Getenv("MERCH_BACKTEST_CSV"))
	type tally struct {
		n, within25                            int
		feeAbs, feeBias, weightAbs, weightBias float64
	}
	stats := map[string]*tally{}
	add := func(key string, weight, quoted float64, label float64, actual int) {
		if stats[key] == nil {
			stats[key] = &tally{}
		}
		s := stats[key]
		s.n++
		d := quoted - float64(actual)
		s.feeAbs += math.Abs(d)
		s.feeBias += d
		s.weightAbs += math.Abs(weight - label)
		s.weightBias += weight - label
		if math.Abs(d) <= 25 {
			s.within25++
		}
	}
	refused := 0
	for _, s := range input.Shipments {
		if s.QuotedWeightOz <= 0 || s.LabelWeightOz <= 0 || s.ActualCents <= 0 {
			t.Fatal("incomplete quote/label pair")
		}
		add("original, all", s.QuotedWeightOz, float64(s.ChargedCents), s.LabelWeightOz, s.ActualCents)
		p, err := Pack(s.Lines, catalog, IsInternational(s.Country))
		if err != nil {
			refused++
			continue
		}
		created, err := time.Parse(time.DateOnly, s.CreatedOn)
		if err != nil {
			t.Fatal(err)
		}
		var before []PastShipment
		for _, h := range ships {
			if h.ShippedOn.Before(created) {
				before = append(before, h)
			}
		}
		e := NewHistory(before).Estimate(s.Country, p, created)
		if e.PostageSamples == 0 {
			t.Fatal("no postage history for replay")
		}
		for _, k := range []string{"all", s.Country + " " + s.Class, usBand(s.LabelWeightOz)} {
			add("original, comparable "+k, s.QuotedWeightOz, float64(s.ChargedCents), s.LabelWeightOz, s.ActualCents)
			add("replayed, "+k, p.WeightOz, float64(e.PostageTypicalCents), s.LabelWeightOz, s.ActualCents)
		}
	}
	keys := []string{}
	for k := range stats {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		s := stats[k]
		n := float64(s.n)
		t.Logf("%s: n=%d within25=%d fee MAE=%.1f¢ bias=%+.1f¢ packed weight MAE=%.2foz bias=%+.2foz", k, s.n, s.within25, s.feeAbs/n, s.feeBias/n, s.weightAbs/n, s.weightBias/n)
	}
	t.Logf("refused by corrected packing: %d of %d (excluded from comparable/replayed rows)", refused, len(input.Shipments))
}
