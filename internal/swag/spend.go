package swag

import (
	"math"
	"sort"
	"strings"
	"time"
)

// SpendRow is one shipped request, for the admin dashboard: when it shipped
// and what it cost Hack Club (the warehouse's actual figures where it has them).
type SpendRow struct {
	At            time.Time
	Country       string
	Costs         Costs
	PostageActual bool // Costs.PostageCents is the label's price, not an estimate
	FeePaidCents  int  // what the requester paid for postage, less refunds
}

// DaySpend is what went out on one day (in New York), by kind of cost.
type DaySpend struct {
	Day          string `json:"day"` // YYYY-MM-DD; "" for a total
	Requests     int    `json:"requests"`
	ItemsCents   int    `json:"items_cents"`
	LaborCents   int    `json:"labor_cents"`
	PackingCents int    `json:"packing_cents"`
	PostageCents int    `json:"postage_cents"`
	TotalCents   int    `json:"total_cents"`
	// PostageActual counts the requests whose postage is the label's real price.
	PostageActual int `json:"postage_actual"`
}

func (d *DaySpend) add(r SpendRow) {
	c := r.Costs
	d.Requests++
	if r.PostageActual {
		d.PostageActual++
	}
	d.ItemsCents += c.ItemsCents
	d.LaborCents += c.LaborCents
	d.PackingCents += c.PackingCents
	d.PostageCents += c.PostageCents
	d.TotalCents += c.TotalCents
}

// MonthSpend is the month so far and where it's heading: the month's spend
// plus the last 7 days' daily average for each day left after today.
type MonthSpend struct {
	Month          string `json:"month"` // YYYY-MM
	SpentCents     int    `json:"spent_cents"`
	Requests       int    `json:"requests"`
	DailyRateCents int    `json:"daily_rate_cents"`
	DaysLeft       int    `json:"days_left"`
	ProjectedCents int    `json:"projected_cents"`
}

// Offset is whether international requesters' postage payments cover what
// their packages cost to mail. Net is collected minus postage; Other is the
// rest of what those packages cost (items, labor, packing), which Hack Club
// pays wherever they go.
type Offset struct {
	Country        string `json:"country,omitempty"`
	Requests       int    `json:"requests"`
	CollectedCents int    `json:"collected_cents"`
	PostageCents   int    `json:"postage_cents"`
	PostageActual  int    `json:"postage_actual"` // how many postage figures are the real label price
	OtherCents     int    `json:"other_cents"`
	NetCents       int    `json:"net_cents"`
}

func (o *Offset) add(r SpendRow) {
	o.Requests++
	o.CollectedCents += r.FeePaidCents
	o.PostageCents += r.Costs.PostageCents
	if r.PostageActual {
		o.PostageActual++
	}
	o.OtherCents += r.Costs.TotalCents - r.Costs.PostageCents
	o.NetCents = o.CollectedCents - o.PostageCents
}

type SpendSummary struct {
	Days          []DaySpend `json:"days"` // every day in the window, oldest first
	Totals        DaySpend   `json:"totals"`
	Month         MonthSpend `json:"month"`
	International Offset     `json:"international"`
	ByCountry     []Offset   `json:"by_country"` // most requests first
}

// SummarizeSpend adds up rows by day over the last days days up to now (all
// of them when days <= 0), the month so far with a projection, and how
// international payments compare with their postage over the same days.
func SummarizeSpend(rows []SpendRow, days int, now time.Time) SpendSummary {
	day := func(t time.Time) time.Time {
		t = t.In(shipZone)
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, shipZone)
	}
	today := day(now)
	from := today.AddDate(0, 0, 1-days)
	if days <= 0 {
		from = today
		for _, r := range rows {
			if d := day(r.At); d.Before(from) {
				from = d
			}
		}
	}
	s := SpendSummary{ByCountry: []Offset{}}
	index := map[string]int{}
	for d := from; !d.After(today); d = d.AddDate(0, 0, 1) {
		index[d.Format(time.DateOnly)] = len(s.Days)
		s.Days = append(s.Days, DaySpend{Day: d.Format(time.DateOnly)})
	}

	monthStart := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, shipZone)
	weekStart := today.AddDate(0, 0, -6)
	week := 0
	countries := map[string]*Offset{}
	for _, r := range rows {
		d := day(r.At)
		if !d.Before(monthStart) && !d.After(today) {
			s.Month.SpentCents += r.Costs.TotalCents
			s.Month.Requests++
		}
		if !d.Before(weekStart) && !d.After(today) {
			week += r.Costs.TotalCents
		}
		i, ok := index[d.Format(time.DateOnly)]
		if !ok {
			continue
		}
		s.Days[i].add(r)
		s.Totals.add(r)
		if IsInternational(r.Country) {
			s.International.add(r)
			c := strings.ToUpper(strings.TrimSpace(r.Country))
			if countries[c] == nil {
				countries[c] = &Offset{Country: c}
			}
			countries[c].add(r)
		}
	}
	for _, o := range countries {
		s.ByCountry = append(s.ByCountry, *o)
	}
	sort.Slice(s.ByCountry, func(i, j int) bool {
		a, b := s.ByCountry[i], s.ByCountry[j]
		if a.Requests != b.Requests {
			return a.Requests > b.Requests
		}
		return a.Country < b.Country
	})

	s.Month.Month = today.Format("2006-01")
	s.Month.DaysLeft = monthStart.AddDate(0, 1, -1).Day() - today.Day()
	rate := float64(week) / 7
	s.Month.DailyRateCents = int(math.Round(rate))
	s.Month.ProjectedCents = s.Month.SpentCents + int(math.Round(rate*float64(s.Month.DaysLeft)))
	return s
}
