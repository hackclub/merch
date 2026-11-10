package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"time"

	"github.com/hackclub/merch/internal/swag"
)

// Only packing inputs enter the replay. In particular, copying a full catalog
// response must not carry descriptions, photos or other unrelated fields along.
type replayItem struct {
	ID          int64          `json:"id"`
	SKU         string         `json:"sku"`
	WidthIn     *float64       `json:"width_in,omitempty"`
	HeightIn    *float64       `json:"height_in,omitempty"`
	ThicknessIn *float64       `json:"thickness_in,omitempty"`
	WeightOz    *float64       `json:"weight_oz,omitempty"`
	Variants    []swag.Variant `json:"variants,omitempty"`
	Packs       string         `json:"packs,omitempty"`
	Binding     string         `json:"binding,omitempty"`
	ShipsWith   []string       `json:"ships_with,omitempty"`
}

type replayShipment struct {
	Country        string      `json:"country"`
	Class          string      `json:"class"`
	CreatedOn      string      `json:"created_on"`
	ChargedCents   int         `json:"charged_cents"`
	ActualCents    int         `json:"actual_cents"`
	Lines          []swag.Line `json:"lines"`
	QuotedWeightOz float64     `json:"quoted_weight_oz"`
	LabelWeightOz  float64     `json:"label_weight_oz"`
}
type replayDocument struct {
	Items     []replayItem     `json:"items"`
	Shipments []replayShipment `json:"shipments"`
}
type replayStats struct{ Paired, Changed, Incomplete, Unmatched, CostsDisagree int }

func replayRows(r io.Reader, required ...string) ([]map[string]string, error) {
	c := csv.NewReader(r)
	header, err := c.Read()
	if err != nil {
		return nil, err
	}
	for _, name := range required {
		found := false
		for _, h := range header {
			found = found || h == name
		}
		if !found {
			return nil, fmt.Errorf("export missing column %s", name)
		}
	}
	var rows []map[string]string
	for {
		rec, err := c.Read()
		if err == io.EOF {
			return rows, nil
		}
		if err != nil {
			return nil, err
		}
		row := map[string]string{}
		for i, h := range header {
			row[h] = rec[i]
		}
		rows = append(rows, row)
	}
}

func replayNumber(s string) (float64, error) {
	n, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n <= 0 {
		return 0, fmt.Errorf("export needs a positive finite weight")
	}
	return n, nil
}

func generateReplay(catalog, requests, labels io.Reader, scope string) (replayDocument, replayStats, error) {
	out := replayDocument{Shipments: []replayShipment{}}
	stats := replayStats{}
	if scope != "all" && scope != "international" && scope != "US" {
		return out, stats, fmt.Errorf("scope must be all, international or US")
	}
	if err := json.NewDecoder(catalog).Decode(&out.Items); err != nil {
		return out, stats, fmt.Errorf("catalog JSON: %w", err)
	}
	items := map[int64]bool{}
	for _, it := range out.Items {
		items[it.ID] = true
	}
	rows, err := replayRows(labels, "order_number", "country", "class", "weight_oz", "postage_cents")
	if err != nil {
		return out, stats, err
	}
	type label struct {
		country, class string
		oz             float64
		cents          int
	}
	byOrder := map[string]label{}
	ambiguous := map[string]bool{}
	for _, r := range rows {
		if r["order_number"] == "" {
			continue
		}
		oz, err := replayNumber(r["weight_oz"])
		if err != nil {
			return out, stats, err
		}
		cents, err := strconv.Atoi(r["postage_cents"])
		if err != nil || cents <= 0 {
			return out, stats, fmt.Errorf("label export needs positive postage cents")
		}
		l := label{r["country"], r["class"], oz, cents}
		if l.class != swag.ClassLetter && l.class != swag.ClassParcel {
			return out, stats, fmt.Errorf("label export has invalid class")
		}
		key := r["order_number"]
		if prev, ok := byOrder[key]; ok && prev != l {
			ambiguous[key] = true
		}
		byOrder[key] = l
	}
	rows, err = replayRows(requests, "id", "country", "created_on", "order_number", "charged_cents", "quoted_cents", "quoted_weight_oz", "actual_cents", "lines", "unchanged")
	if err != nil {
		return out, stats, err
	}
	seen := map[string]bool{}
	for _, r := range rows {
		country := r["country"]
		if (scope == "US" && country != "US") || (scope == "international" && country == "US") {
			continue
		}
		if seen[r["id"]] {
			return out, stats, fmt.Errorf("request export contains duplicate rows")
		}
		seen[r["id"]] = true
		if r["unchanged"] != "true" && r["unchanged"] != "t" {
			stats.Changed++
			continue
		}
		if r["quoted_weight_oz"] == "" || r["quoted_cents"] == "" || r["actual_cents"] == "" {
			stats.Incomplete++
			continue
		}
		l, ok := byOrder[r["order_number"]]
		if !ok || ambiguous[r["order_number"]] || l.country != country {
			stats.Unmatched++
			continue
		}
		actual, err := strconv.Atoi(r["actual_cents"])
		if err != nil || actual <= 0 {
			return out, stats, fmt.Errorf("request export needs positive actual postage cents")
		}
		if actual != l.cents {
			stats.CostsDisagree++
			continue
		}
		weight, err := replayNumber(r["quoted_weight_oz"])
		if err != nil {
			return out, stats, err
		}
		charged := r["charged_cents"]
		// US shipping is free. Compare its original postage estimate to the
		// warehouse cost; abroad compare the fee collected, net of refunds.
		if country == "US" {
			charged = r["quoted_cents"]
		}
		cents, err := strconv.Atoi(charged)
		if err != nil || cents < 0 {
			return out, stats, fmt.Errorf("request export has invalid charged/quoted cents")
		}
		if _, err := time.Parse(time.DateOnly, r["created_on"]); err != nil {
			return out, stats, fmt.Errorf("request export needs YYYY-MM-DD created_on")
		}
		var lines []swag.Line
		if err := json.Unmarshal([]byte(r["lines"]), &lines); err != nil {
			return out, stats, fmt.Errorf("request export has invalid lines JSON")
		}
		if len(lines) == 0 {
			return out, stats, fmt.Errorf("request export has no lines")
		}
		for _, line := range lines {
			if !items[line.ItemID] || line.Quantity < 0 {
				return out, stats, fmt.Errorf("request export refers to an absent item or invalid quantity")
			}
		}
		out.Shipments = append(out.Shipments, replayShipment{Country: country, Class: l.class, CreatedOn: r["created_on"], ChargedCents: cents, ActualCents: actual, Lines: lines, QuotedWeightOz: weight, LabelWeightOz: l.oz})
		stats.Paired++
	}
	return out, stats, nil
}

func replay(catalogPath, requestPath, labelPath, scope string, output, summary io.Writer) error {
	var files []*os.File
	defer func() {
		for _, f := range files {
			f.Close()
		}
	}()
	for _, path := range []string{catalogPath, requestPath, labelPath} {
		if path == "" {
			return fmt.Errorf("replay requires -catalog, -requests and -labels")
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		files = append(files, f)
	}
	out, stats, err := generateReplay(files[0], files[1], files[2], scope)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(output).Encode(out); err != nil {
		return err
	}
	fmt.Fprintf(summary, "replay: %+v\n", stats)
	return nil
}
