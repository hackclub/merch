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
	Suggest(ctx context.Context, input, session string) ([]places.Suggestion, error)
	Lookup(ctx context.Context, placeID, session string) (swag.Address, error)
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
	out, err := s.Places.Suggest(r.Context(), q, r.URL.Query().Get("session"))
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
