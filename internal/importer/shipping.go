package importer

import (
	"encoding/csv"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Shipment is one package mailed before, for shipping estimates. It comes from
// the data warehouse via scripts/shipping-history/*.sql, as CSV lines:
//
//	source,country,class,weight_oz,postage_cents,ordered_on,shipped_on,delivered_on,tracking
//
// Empty fields are unknown. Tracking is only kept to look up delivery dates
// (cmd/shiphistory track); it is never stored.
type Shipment struct {
	Source       string
	Country      string
	Class        string // swag.ClassLetter or swag.ClassParcel
	WeightOz     *float64
	PostageCents *int
	OrderedOn    *time.Time
	ShippedOn    time.Time
	DeliveredOn  *time.Time
	Tracking     string
}

var ShipmentColumns = []string{"source", "country", "class", "weight_oz", "postage_cents", "ordered_on", "shipped_on", "delivered_on", "tracking"}

var alpha2 = regexp.MustCompile(`^[A-Z]{2}$`)

// ParseShipments reads shipment lines; a header line is optional.
func ParseShipments(r io.Reader) ([]Shipment, error) {
	cr := newCSVReader(r)
	var out []Shipment
	for line := 1; ; line++ {
		rec, err := cr.Read()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		if line == 1 && rec[0] == "source" {
			continue
		}
		s, err := parseShipment(rec)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}
		out = append(out, s)
	}
}

func parseShipment(rec []string) (Shipment, error) {
	if len(rec) < 7 {
		return Shipment{}, fmt.Errorf("want %d columns, got %d", len(ShipmentColumns), len(rec))
	}
	for len(rec) < len(ShipmentColumns) {
		rec = append(rec, "")
	}
	for i := range rec {
		rec[i] = strings.TrimSpace(rec[i])
	}
	s := Shipment{Source: rec[0], Country: rec[1], Class: rec[2], Tracking: rec[8]}
	if s.Source == "" || !alpha2.MatchString(s.Country) || (s.Class != "letter" && s.Class != "parcel") {
		return s, fmt.Errorf("bad source, country or class: %q", strings.Join(rec[:3], ","))
	}
	if rec[3] != "" {
		v, err := strconv.ParseFloat(rec[3], 64)
		if err != nil {
			return s, fmt.Errorf("weight_oz: %w", err)
		}
		s.WeightOz = &v
	}
	if rec[4] != "" {
		v, err := strconv.Atoi(rec[4])
		if err != nil {
			return s, fmt.Errorf("postage_cents: %w", err)
		}
		s.PostageCents = &v
	}
	var err error
	if s.OrderedOn, err = optDate(rec[5]); err != nil {
		return s, fmt.Errorf("ordered_on: %w", err)
	}
	if s.ShippedOn, err = time.Parse(time.DateOnly, rec[6]); err != nil {
		return s, fmt.Errorf("shipped_on: %w", err)
	}
	if s.DeliveredOn, err = optDate(rec[7]); err != nil {
		return s, fmt.Errorf("delivered_on: %w", err)
	}
	return s, nil
}

func optDate(v string) (*time.Time, error) {
	if v == "" {
		return nil, nil
	}
	t, err := time.Parse(time.DateOnly, v)
	return &t, err
}

// WriteShipments writes shipments as CSV with a header, the format
// ParseShipments reads. withTracking false leaves the tracking column empty.
func WriteShipments(w io.Writer, ships []Shipment, withTracking bool) error {
	cw := csv.NewWriter(w)
	cw.Write(ShipmentColumns)
	date := func(t *time.Time) string {
		if t == nil {
			return ""
		}
		return t.Format(time.DateOnly)
	}
	for _, s := range ships {
		rec := []string{s.Source, s.Country, s.Class, "", "", date(s.OrderedOn), s.ShippedOn.Format(time.DateOnly), date(s.DeliveredOn), ""}
		if s.WeightOz != nil {
			rec[3] = strconv.FormatFloat(*s.WeightOz, 'f', -1, 64)
		}
		if s.PostageCents != nil {
			rec[4] = strconv.Itoa(*s.PostageCents)
		}
		if withTracking {
			rec[8] = s.Tracking
		}
		cw.Write(rec)
	}
	cw.Flush()
	return cw.Error()
}
