package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hackclub/merch/internal/swag"
)

// historyTTL bounds how often estimates reload shipping history (it changes
// only when someone runs cmd/shiphistory load).
const historyTTL = time.Hour

type historyCache struct {
	mu   sync.Mutex
	rows []swag.PastShipment
	at   time.Time
}

func (s *server) shippingHistory(ctx context.Context) ([]swag.PastShipment, error) {
	s.history.mu.Lock()
	defer s.history.mu.Unlock()
	if s.history.rows != nil && s.Now().Sub(s.history.at) < historyTTL {
		return s.history.rows, nil
	}
	rows, err := s.Store.ShippingHistory(ctx)
	if err != nil {
		return nil, err
	}
	if rows == nil {
		rows = []swag.PastShipment{}
	}
	s.history.rows, s.history.at = rows, s.Now()
	return rows, nil
}

// forViewer hides the postage figures from everyone but admins; requesters see
// only FeeCents, what they'll pay.
func forViewer(e swag.Estimate, admin bool) swag.Estimate {
	if !admin {
		e.PostageCents, e.PostageTypicalCents, e.PostageBasis, e.PostageSamples = [2]int{}, 0, "", 0
	}
	return e
}

// quote packs lines and estimates their shipping to country, with the fee the
// requester owes. A request that won't pack gets a zero estimate (Validate
// refuses it anyway).
func (s *server) quote(ctx context.Context, st swag.Settings, country string, lines []swag.Line, cat map[int64]swag.Item) (swag.Packing, swag.Estimate, error) {
	h, err := s.shippingHistory(ctx)
	if err != nil {
		return swag.Packing{}, swag.Estimate{}, err
	}
	p, err := swag.Pack(lines, cat, swag.IsInternational(country))
	if err != nil {
		return p, swag.Estimate{}, err
	}
	e := swag.EstimateShipping(h, country, p, s.Now())
	_, e.FeeCents = swag.InitialStatus(st, country, e)
	return p, e, nil
}

func (s *server) isAdmin(r *http.Request) bool {
	return s.rolesFor(r.Context(), s.currentUser(r)).Admin
}

// shippingEstimate packs a would-be request and estimates its delivery, for the
// request form. 422 means it can't ship as one package (or is over 3 lb abroad).
func (s *server) shippingEstimate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Country string      `json:"country"`
		Lines   []swag.Line `json:"lines"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	country := strings.ToUpper(strings.TrimSpace(body.Country))
	if country == "" {
		country = "US"
	}
	items, err := s.Store.ListItems(r.Context(), true)
	if err != nil {
		s.serverErr(w, err)
		return
	}
	cat := map[int64]swag.Item{}
	for _, it := range items {
		cat[it.ID] = it
	}
	var lines []swag.Line
	for _, l := range body.Lines {
		if _, ok := cat[l.ItemID]; !ok || l.Quantity < 1 {
			s.track(r, s.currentUser(r), "shipping_estimate", map[string]any{"country": country, "lines": body.Lines, "error": swag.ErrUnknownItem.Error()})
			writeErr(w, http.StatusUnprocessableEntity, swag.ErrUnknownItem.Error())
			return
		}
		lines = append(lines, l)
	}
	if len(lines) == 0 {
		writeErr(w, http.StatusUnprocessableEntity, swag.ErrNoItems.Error())
		return
	}
	st, err := s.Store.GetSettings(r.Context())
	if err != nil {
		s.serverErr(w, err)
		return
	}
	p, e, err := s.quote(r.Context(), st, country, lines, cat)
	u := s.currentUser(r)
	if errors.Is(err, swag.ErrWontPack) || errors.Is(err, swag.ErrTooHeavy) {
		s.track(r, u, "shipping_estimate", map[string]any{"country": country, "lines": lines, "items": itemCount(lines), "error": err.Error()})
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	} else if err != nil {
		s.serverErr(w, err)
		return
	}
	props := estimateProps(country, lines, p, e)
	props["items"] = itemCount(lines)
	s.track(r, u, "shipping_estimate", props)
	admin := s.rolesFor(r.Context(), u).Admin
	out := map[string]any{"estimate": forViewer(e, admin), "packing": p}
	if admin {
		if out["costs"], err = s.costs(r.Context(), st, country, lines, cat); err != nil {
			s.serverErr(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// costs is what lines cost Hack Club to send in the US and abroad (to country
// if it's abroad, else anywhere abroad), for admins. A side that can't ship
// as one package (over 3 lb abroad) is nil.
func (s *server) costs(ctx context.Context, st swag.Settings, country string, lines []swag.Line, cat map[int64]swag.Item) (map[string]*swag.Costs, error) {
	var shipped []swag.Shipped
	shipsWith := map[int64][]string{}
	for _, l := range lines {
		sku := l.SKU
		if sku == "" {
			sku = cat[l.ItemID].SKU
		}
		shipped = append(shipped, swag.Shipped{ItemID: l.ItemID, SKU: sku, Quantity: l.Quantity})
		shipsWith[l.ItemID] = cat[l.ItemID].ShipsWith
	}
	contents := swag.Shipment(shipped, shipsWith)
	// Without the warehouse, every SKU just shows as unpriced.
	unit := map[string]float64{}
	if skus, err := s.cachedSKUs(ctx); err != nil {
		s.Log.Error("warehouse sku lookup failed", "err", err)
	} else {
		for _, k := range skus {
			if c, err := strconv.ParseFloat(k.UnitCost, 64); err == nil {
				unit[k.SKU] = c
			}
		}
	}
	abroad := country
	if !swag.IsInternational(abroad) {
		abroad = ""
	}
	out := map[string]*swag.Costs{}
	for key, to := range map[string]string{"us": "US", "international": abroad} {
		p, e, err := s.quote(ctx, st, to, lines, cat)
		if errors.Is(err, swag.ErrWontPack) || errors.Is(err, swag.ErrTooHeavy) {
			out[key] = nil
			continue
		} else if err != nil {
			return nil, err
		}
		c := swag.CostToShip(contents, unit, p, e)
		out[key] = &c
	}
	return out, nil
}

// shippingSummary is the typical delivery time in the US and abroad, and the
// typical international fee, for the shop's intro. "Typical" is a half-pound
// parcel, about what Asendia usually carries for us.
func (s *server) shippingSummary(w http.ResponseWriter, r *http.Request) {
	h, err := s.shippingHistory(r.Context())
	if err != nil {
		s.serverErr(w, err)
		return
	}
	st, err := s.Store.GetSettings(r.Context())
	if err != nil {
		s.serverErr(w, err)
		return
	}
	parcel := swag.Packing{Package: swag.Package{Name: "Bubble mailer", Class: swag.ClassParcel}, ContentsOz: 8, WeightOz: 9}
	us := swag.EstimateShipping(h, "US", parcel, s.Now())
	intl := swag.EstimateShipping(h, "", parcel, s.Now())
	_, intl.FeeCents = swag.InitialStatus(st, "", intl)
	admin := s.isAdmin(r)
	writeJSON(w, http.StatusOK, map[string]any{"us": forViewer(us, admin), "international": forViewer(intl, admin)})
}

// withEstimates adds delivery estimates to requests still waiting to ship.
func (s *server) withEstimates(ctx context.Context, views []requestView, admin bool) error {
	var cat map[int64]swag.Item
	var h []swag.PastShipment
	for i, v := range views {
		if v.Source != "app" || (v.Status != swag.StatusPending && v.Status != swag.StatusAwaitingPayment) {
			continue
		}
		if cat == nil {
			items, err := s.Store.ListItems(ctx, false)
			if err != nil {
				return err
			}
			cat = map[int64]swag.Item{}
			for _, it := range items {
				cat[it.ID] = it
			}
			if h, err = s.shippingHistory(ctx); err != nil {
				return err
			}
		}
		lines := make([]swag.Line, len(v.Lines))
		for j, l := range v.Lines {
			lines[j] = swag.Line{ItemID: l.ItemID, Quantity: l.Quantity, SKU: l.SKU}
		}
		// Requests made before the packing rules may not pack; skip those.
		p, err := swag.Pack(lines, cat, swag.IsInternational(v.Address.Country))
		if err != nil {
			continue
		}
		e := forViewer(swag.EstimateShipping(h, v.Address.Country, p, s.Now()), admin)
		views[i].Estimate = &e
	}
	return nil
}
