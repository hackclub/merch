package httpapi

import (
	"context"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/hackclub/merch/internal/places"
	"github.com/hackclub/merch/internal/swag"
)

// AddressFinder suggests shipping addresses as people type (Google Places).
type AddressFinder interface {
	Suggest(ctx context.Context, input, session string, near places.Near) ([]places.Suggestion, error)
	Lookup(ctx context.Context, placeID, session string) (swag.Address, error)
	Locate(ctx context.Context, text string) (lat, lng float64, ok bool, err error)
}

// Shorter queries match too much to be worth a billed call.
const minAddressQuery = 4

func (s *server) addressSuggest(w http.ResponseWriter, r *http.Request, v viewer) {
	if s.Places == nil {
		writeErr(w, http.StatusNotFound, "address search is off")
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if utf8.RuneCountInString(q) < minAddressQuery {
		writeJSON(w, http.StatusOK, map[string]any{"suggestions": []places.Suggestion{}})
		return
	}
	if len(q) > 200 {
		q = q[:200]
	}
	out, err := s.Places.Suggest(r.Context(), q, r.URL.Query().Get("session"), s.nearViewer(r.Context(), v, r.URL.Query().Get("region")))
	if err != nil {
		s.Log.Warn("address suggest", "err", err)
		writeErr(w, http.StatusBadGateway, "address search failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"suggestions": out})
}

func (s *server) addressPlace(w http.ResponseWriter, r *http.Request, v viewer) {
	if s.Places == nil {
		writeErr(w, http.StatusNotFound, "address search is off")
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		writeErr(w, http.StatusBadRequest, "id is required")
		return
	}
	a, err := s.Places.Lookup(r.Context(), id, r.URL.Query().Get("session"))
	if err != nil {
		s.Log.Warn("address lookup", "err", err)
		writeErr(w, http.StatusBadGateway, "address lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, a)
}

// nearViewer guesses where the person is, so their own area's addresses come first:
// around the town of their Hack Club Auth address when they have one (only the town
// is looked up, never the street), else the region of their browser's language.
func (s *server) nearViewer(ctx context.Context, v viewer, browserRegion string) places.Near {
	var near places.Near
	if r := strings.ToUpper(strings.TrimSpace(browserRegion)); len(r) == 2 && r[0] >= 'A' && r[0] <= 'Z' && r[1] >= 'A' && r[1] <= 'Z' {
		near.Region = r
	}
	a := v.user.Address
	if a == nil || strings.TrimSpace(a.Country) == "" {
		return near
	}
	near.Region = strings.ToUpper(strings.TrimSpace(a.Country))
	town := strings.TrimSpace(strings.Join(nonEmpty(a.City, strings.TrimSpace(a.State+" "+a.PostalCode)), ", "))
	if town == "" {
		return near
	}
	lat, lng, ok, err := s.Places.Locate(ctx, town+", "+near.Region)
	if err != nil {
		s.Log.Warn("address locate", "err", err)
	} else if ok {
		near.Lat, near.Lng = lat, lng
	}
	return near
}

func nonEmpty(xs ...string) []string {
	var out []string
	for _, x := range xs {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}
