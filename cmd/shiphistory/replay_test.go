package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
)

func replayCSV(rows ...[]string) string {
	var b bytes.Buffer
	w := csv.NewWriter(&b)
	w.WriteAll(rows)
	return b.String()
}

func TestGenerateReplay(t *testing.T) {
	catalog := `[{"id":1,"sku":"Sti/Test","packs":"sticker","weight_oz":0.05,"private_note":"never export this"}]`
	requests := replayCSV(
		[]string{"id", "country", "created_on", "order_number", "charged_cents", "quoted_cents", "quoted_weight_oz", "actual_cents", "lines", "unchanged"},
		[]string{"example-intl", "CA", "2026-11-01", "parcel-a", "950", "950", "4", "1000", `[{"item_id":1,"sku":"Sti/Test","quantity":50}]`, "true"},
		[]string{"example-us", "US", "2026-11-02", "parcel-b", "0", "500", "3", "525", `[{"item_id":1,"sku":"Sti/Test","quantity":40}]`, "true"},
		[]string{"changed", "CA", "2026-11-01", "parcel-a", "950", "950", "4", "1000", `[{"item_id":1,"quantity":30}]`, "false"},
		[]string{"missing-quote", "CA", "2026-11-01", "parcel-a", "950", "", "", "1000", `[{"item_id":1,"quantity":30}]`, "true"},
	)
	labels := replayCSV(
		[]string{"order_number", "country", "class", "weight_oz", "postage_cents"},
		[]string{"parcel-a", "CA", "parcel", "4.2", "1000"},
		[]string{"parcel-a", "CA", "parcel", "4.2", "1000"}, // identical duplicate is safe
		[]string{"parcel-b", "US", "parcel", "3.2", "525"},
	)
	for _, scope := range []string{"all", "international", "US"} {
		t.Run(scope, func(t *testing.T) {
			out, stats, err := generateReplay(strings.NewReader(catalog), strings.NewReader(requests), strings.NewReader(labels), scope)
			if err != nil {
				t.Fatal(err)
			}
			want := 2
			if scope != "all" {
				want = 1
			}
			if len(out.Shipments) != want {
				t.Fatalf("shipments = %d, want %d", len(out.Shipments), want)
			}
			for _, s := range out.Shipments {
				fee := 950
				if s.Country == "US" {
					fee = 500
				}
				if s.ChargedCents != fee || len(s.Lines) != 1 || s.LabelWeightOz <= 0 {
					t.Fatalf("bad pair: %+v", s)
				}
			}
			if scope == "all" && (stats.Changed != 1 || stats.Incomplete != 1) {
				t.Fatalf("skips: %+v", stats)
			}
			b, err := json.Marshal(out)
			if err != nil {
				t.Fatal(err)
			}
			for _, private := range []string{"order_number", "parcel-a", "example-intl", "private_note", "never export"} {
				if bytes.Contains(b, []byte(private)) {
					t.Errorf("output includes %q", private)
				}
			}
		})
	}
}

func TestGenerateReplayRejectsAmbiguousLabels(t *testing.T) {
	requests := replayCSV([]string{"id", "country", "created_on", "order_number", "charged_cents", "quoted_cents", "quoted_weight_oz", "actual_cents", "lines", "unchanged"},
		[]string{"example", "CA", "2026-11-01", "duplicate", "500", "500", "2", "500", `[{"item_id":1,"quantity":1}]`, "true"})
	labels := "order_number,country,class,weight_oz,postage_cents\nduplicate,CA,parcel,2,500\nduplicate,CA,parcel,3,600\n"
	out, stats, err := generateReplay(strings.NewReader(`[{"id":1,"sku":"Sti/Test"}]`), strings.NewReader(requests), strings.NewReader(labels), "all")
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Shipments) != 0 || stats.Unmatched != 1 {
		t.Fatalf("ambiguous pair accepted: %+v %+v", out, stats)
	}
}
