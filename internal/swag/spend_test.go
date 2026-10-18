package swag

import (
	"testing"
	"time"
)

func TestSummarizeSpend(t *testing.T) {
	ny := shipZone
	at := func(day, hour int) time.Time { return time.Date(2026, 10, day, hour, 0, 0, 0, ny) }
	now := at(17, 15)
	cost := func(items, labor, packing, postage int) Costs {
		return Costs{ItemsCents: items, LaborCents: labor, PackingCents: packing, PostageCents: postage, TotalCents: items + labor + packing + postage}
	}
	rows := []SpendRow{
		{At: at(17, 9), Country: "US", Costs: cost(100, 200, 50, 500)},
		{At: at(17, 1), Country: "us", Costs: cost(0, 200, 0, 400)},
		// Paid $19.25 for postage that cost $15, plus $3 of items and $2 of labor.
		{At: at(16, 12), Country: "DE", Costs: cost(300, 200, 0, 1500), PostageActual: true, FeePaidCents: 1925},
		// Paid $8 for $9.50 of postage, $2 of it refunded after shipping less.
		{At: at(12, 12), Country: "IN", Costs: cost(0, 200, 0, 950), FeePaidCents: 600},
		{At: at(3, 12), Country: "US", Costs: cost(1000, 0, 0, 0)},
		// Last month: out of the month, and out of a 7-day window.
		{At: time.Date(2026, 9, 30, 12, 0, 0, 0, ny), Country: "DE", Costs: cost(0, 0, 0, 900), FeePaidCents: 900},
	}
	s := SummarizeSpend(rows, 7, now)

	if len(s.Days) != 7 || s.Days[0].Day != "2026-10-11" || s.Days[6].Day != "2026-10-17" {
		t.Fatalf("days = %+v", s.Days)
	}
	today := s.Days[6]
	if today.Requests != 2 || today.ItemsCents != 100 || today.LaborCents != 400 || today.PackingCents != 50 || today.PostageCents != 900 || today.TotalCents != 1450 {
		t.Errorf("today = %+v", today)
	}
	if s.Days[2].Requests != 0 || s.Days[1].TotalCents != 1150 {
		t.Errorf("Oct 12 = %+v, Oct 13 = %+v", s.Days[1], s.Days[2])
	}
	if s.Totals.Requests != 4 || s.Totals.TotalCents != 1450+2000+1150 || s.Totals.PostageActual != 1 {
		t.Errorf("totals = %+v", s.Totals)
	}

	// October so far: 4600 + 1000 on the 3rd. The last 7 days spent 4600, so
	// the 14 days left add 14 × 4600/7.
	m := s.Month
	if m.Month != "2026-10" || m.SpentCents != 5600 || m.Requests != 5 || m.DailyRateCents != 657 || m.DaysLeft != 14 || m.ProjectedCents != 5600+9200 {
		t.Errorf("month = %+v", m)
	}

	in := s.International
	if in.Requests != 2 || in.CollectedCents != 2525 || in.PostageCents != 2450 || in.PostageActual != 1 || in.OtherCents != 700 || in.NetCents != 75 {
		t.Errorf("international = %+v", in)
	}
	if len(s.ByCountry) != 2 || s.ByCountry[0].Country != "DE" || s.ByCountry[0].NetCents != 425 || s.ByCountry[1].NetCents != -350 {
		t.Errorf("by country = %+v", s.ByCountry)
	}
}

// With no window, it covers every day since the first shipment.
func TestSummarizeSpendAllTime(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, shipZone)
	s := SummarizeSpend([]SpendRow{{At: now.AddDate(0, 0, -9), Country: "US", Costs: Costs{TotalCents: 5}}}, 0, now)
	if len(s.Days) != 10 || s.Totals.TotalCents != 5 {
		t.Fatalf("days = %d, totals = %+v", len(s.Days), s.Totals)
	}
	if s := SummarizeSpend(nil, 0, now); len(s.Days) != 1 || s.ByCountry == nil {
		t.Fatalf("empty = %+v", s)
	}
}
