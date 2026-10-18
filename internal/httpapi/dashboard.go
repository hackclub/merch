package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/hackclub/merch/internal/store"
	"github.com/hackclub/merch/internal/swag"
)

// The admin dashboard: what shipping requests costs Hack Club by day, where
// the month is heading, and whether international requesters' postage
// payments cover their postage.

// shippedCost is what a shipped request cost. Actual names the parts that are
// the warehouse's own figures ("items", "labor", "postage"); the rest are
// estimates from today's unit costs and past postage (packing always is).
type shippedCost struct {
	swag.Costs
	Actual []string `json:"actual"`
}

// adminDashboard: GET /api/admin/dashboard?days=30 (0 = since the first shipment).
func (s *server) adminDashboard(w http.ResponseWriter, r *http.Request, v viewer) {
	days := 30
	if d, err := strconv.Atoi(r.URL.Query().Get("days")); err == nil && d >= 0 && d <= 3660 {
		days = d
	}
	now := s.Now()
	// Enough to cover the window, the month so far and the last 7 days (for the projection).
	since := time.Time{}
	if days > 0 {
		since = now.AddDate(0, 0, -max(days, 31)-1)
	}
	shipped, err := s.Store.ListDispatchedSince(r.Context(), since)
	if err != nil {
		s.serverErr(w, err)
		return
	}
	cost, err := s.coster(r.Context())
	if err != nil {
		s.serverErr(w, err)
		return
	}
	rows := make([]swag.SpendRow, 0, len(shipped))
	costs := map[string]shippedCost{}
	for _, req := range shipped {
		c := cost(req)
		costs[strconv.FormatInt(req.ID, 10)] = c
		row := swag.SpendRow{At: *req.DispatchedAt, Country: req.Address.Country, Costs: c.Costs, PostageActual: req.WarehouseCosts.PostageCents != nil}
		if req.PaidAt != nil {
			row.FeePaidCents = req.ShippingFeeCents - req.RefundedCents
		}
		rows = append(rows, row)
	}

	// What's waiting to ship, at today's estimates.
	queue := struct {
		Requests   int `json:"requests"`
		TotalCents int `json:"total_cents"`
	}{}
	for _, st := range []swag.Status{swag.StatusPending, swag.StatusAwaitingPayment} {
		waiting, err := s.Store.ListRequests(r.Context(), st)
		if err != nil {
			s.serverErr(w, err)
			return
		}
		for _, req := range waiting {
			if req.Source != "app" {
				continue
			}
			queue.Requests++
			queue.TotalCents += cost(req).TotalCents
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"summary": swag.SummarizeSpend(rows, days, now), "costs": costs, "queue": queue})
}

// coster prices requests: the warehouse's charges where it has given them,
// else what the request's shipped lines should cost (swag.CostToShip).
func (s *server) coster(ctx context.Context) (func(*store.Request) shippedCost, error) {
	items, err := s.Store.ListItems(ctx, false)
	if err != nil {
		return nil, err
	}
	cat := map[int64]swag.Item{}
	for _, it := range items {
		cat[it.ID] = it
	}
	h, err := s.shippingHistory(ctx)
	if err != nil {
		return nil, err
	}
	unit := s.unitCosts(ctx)
	now := s.Now()
	return func(req *store.Request) shippedCost {
		var lines []swag.Line
		for _, l := range req.Lines {
			if l.Quantity > 0 {
				lines = append(lines, swag.Line{ItemID: l.ItemID, Quantity: l.Quantity, SKU: l.SKU})
			}
		}
		// Imports from before the packing rules may break them (50 magazines
		// abroad weigh over 3 lb): price those as packed for the US, at the
		// destination's postage. One that won't pack at all gets no estimate.
		var e swag.Estimate
		p, err := swag.Pack(lines, cat, swag.IsInternational(req.Address.Country))
		if err != nil {
			p, err = swag.Pack(lines, cat, false)
		}
		if err == nil {
			e = h.Estimate(req.Address.Country, p, now)
		}
		c := shippedCost{Costs: swag.CostToShip(shipment(lines, cat), unit, p, e), Actual: []string{}}
		wc := req.WarehouseCosts
		for _, part := range []struct {
			name   string
			actual *int
			into   *int
		}{{"items", wc.ContentsCents, &c.ItemsCents}, {"labor", wc.LaborCents, &c.LaborCents}, {"postage", wc.PostageCents, &c.PostageCents}} {
			if part.actual != nil {
				*part.into = *part.actual
				c.Actual = append(c.Actual, part.name)
			}
		}
		if wc.ContentsCents != nil {
			c.UnpricedSKUs = []string{}
		}
		c.TotalCents = c.ItemsCents + c.LaborCents + c.PackingCents + c.PostageCents
		return c
	}, nil
}

// refreshLimit bounds how many orders one refresh asks the warehouse about.
const refreshLimit = 60

// adminRefreshAll pulls tracking and charges from mail.hackclub.com for
// shipped requests that don't have their postage yet (nothing pulls them
// automatically): POST /api/admin/requests/refresh.
func (s *server) adminRefreshAll(w http.ResponseWriter, r *http.Request, v viewer) {
	shipped, err := s.Store.ListDispatchedSince(r.Context(), s.Now().AddDate(0, 0, -90))
	if err != nil {
		s.serverErr(w, err)
		return
	}
	out := struct {
		Checked int `json:"checked"`
		Updated int `json:"updated"`
		Mailed  int `json:"mailed"`
		Failed  int `json:"failed"`
		Left    int `json:"left"` // still to check, past refreshLimit
	}{}
	for _, req := range shipped {
		if req.TheseusOrderID == "" || req.WarehouseCosts.PostageCents != nil {
			continue
		}
		if out.Checked == refreshLimit {
			out.Left++
			continue
		}
		out.Checked++
		if _, mailed, err := s.pullOrder(r.Context(), req); err != nil {
			s.Log.Error("warehouse order refresh failed", "request_id", req.ID, "err", err)
			out.Failed++
		} else {
			out.Updated++
			if mailed {
				out.Mailed++
			}
		}
	}
	s.trackAdmin(r, v, "requests_refreshed", map[string]any{"checked": out.Checked, "updated": out.Updated, "mailed": out.Mailed, "failed": out.Failed, "left": out.Left})
	writeJSON(w, http.StatusOK, out)
}

// pullOrder saves a shipped request's tracking and warehouse charges from
// mail.hackclub.com, and says whether it has been mailed (postage known).
func (s *server) pullOrder(ctx context.Context, req *store.Request) (*store.Request, bool, error) {
	o, err := s.Warehouse.GetOrder(ctx, req.TheseusOrderID)
	if err != nil {
		return nil, false, err
	}
	if o.TrackingNumber != req.TrackingNumber || o.Carrier != req.Carrier {
		if req, err = s.Store.SetTracking(ctx, req.ID, o.TrackingNumber, o.Carrier); err != nil {
			return nil, false, err
		}
	}
	c := o.Costs()
	req, err = s.Store.SetWarehouseCosts(ctx, req.ID, c, o.MailedAt)
	return req, c.PostageCents != nil, err
}
