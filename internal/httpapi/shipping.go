package httpapi

import (
	"context"
	"errors"
	"net/http"
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

// forViewer hides postage (what Hack Club pays) from everyone but admins.
func forViewer(e swag.Estimate, admin bool) swag.Estimate {
	if !admin {
		e.PostageCents, e.PostageBasis, e.PostageSamples = [2]int{}, "", 0
	}
	return e
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
			writeErr(w, http.StatusUnprocessableEntity, swag.ErrUnknownItem.Error())
			return
		}
		lines = append(lines, l)
	}
	if len(lines) == 0 {
		writeErr(w, http.StatusUnprocessableEntity, swag.ErrNoItems.Error())
		return
	}
	p, err := swag.Pack(lines, cat, swag.IsInternational(country))
	if errors.Is(err, swag.ErrWontPack) || errors.Is(err, swag.ErrTooHeavy) {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	} else if err != nil {
		s.serverErr(w, err)
		return
	}
	h, err := s.shippingHistory(r.Context())
	if err != nil {
		s.serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"estimate": forViewer(swag.EstimateShipping(h, country, p, s.Now()), s.isAdmin(r)),
		"packing":  p,
	})
}

// shippingSummary is the typical delivery time in the US and abroad, for the
// shop's intro.
func (s *server) shippingSummary(w http.ResponseWriter, r *http.Request) {
	h, err := s.shippingHistory(r.Context())
	if err != nil {
		s.serverErr(w, err)
		return
	}
	parcel := swag.Packing{Package: swag.Packages[1]}
	admin := s.isAdmin(r)
	writeJSON(w, http.StatusOK, map[string]any{
		"us":            forViewer(swag.EstimateShipping(h, "US", parcel, s.Now()), admin),
		"international": forViewer(swag.EstimateShipping(h, "", parcel, s.Now()), admin),
	})
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
