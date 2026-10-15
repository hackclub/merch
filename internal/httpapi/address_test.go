package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/hackclub/merch/internal/authors"
	"github.com/hackclub/merch/internal/places"
	"github.com/hackclub/merch/internal/swag"
)

type fakePlaces struct {
	mu    sync.Mutex
	calls []string
	fail  error
}

func (f *fakePlaces) Suggest(_ context.Context, input, session string) ([]places.Suggestion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "suggest:"+input+":"+session)
	if f.fail != nil {
		return nil, f.fail
	}
	return []places.Suggestion{{PlaceID: "p1", Main: "15 Falls Rd", Secondary: "Shelburne, VT, USA"}}, nil
}

func (f *fakePlaces) Lookup(_ context.Context, id, session string) (swag.Address, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "lookup:"+id+":"+session)
	return swag.Address{Line1: "15 Falls Rd", City: "Shelburne", State: "VT", PostalCode: "05482", Country: "US"}, nil
}

func TestAddressSuggest(t *testing.T) {
	h := newHarness(t)
	var out struct {
		Suggestions []places.Suggestion `json:"suggestions"`
	}
	if code := h.do(h.client(""), "GET", "/api/address/suggest?q=15+falls", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d, want 401", code)
	}
	if code := h.do(h.client(userEmail), "GET", "/api/address/suggest?q=15+falls&session=s1", nil, &out); code != http.StatusOK {
		t.Fatalf("suggest: %d", code)
	}
	if len(out.Suggestions) != 1 || out.Suggestions[0].PlaceID != "p1" {
		t.Fatalf("got %+v", out)
	}
	// Too short to be worth a billed call.
	out.Suggestions = nil
	if code := h.do(h.client(userEmail), "GET", "/api/address/suggest?q=15", nil, &out); code != http.StatusOK || len(out.Suggestions) != 0 {
		t.Fatalf("short query: %d %+v", code, out)
	}
	if len(h.places.calls) != 1 || h.places.calls[0] != "suggest:15 falls:s1" {
		t.Fatalf("calls = %v", h.places.calls)
	}

	h.places.fail = errors.New("boom")
	if code := h.do(h.client(userEmail), "GET", "/api/address/suggest?q=15+falls", nil, nil); code != http.StatusBadGateway {
		t.Fatalf("upstream failure: %d, want 502", code)
	}
}

func TestAddressPlace(t *testing.T) {
	h := newHarness(t)
	var a swag.Address
	if code := h.do(h.client(userEmail), "GET", "/api/address/place?id=p1&session=s1", nil, &a); code != http.StatusOK {
		t.Fatalf("place: %d", code)
	}
	if a.Line1 != "15 Falls Rd" || a.Country != "US" {
		t.Fatalf("got %+v", a)
	}
	if code := h.do(h.client(userEmail), "GET", "/api/address/place", nil, nil); code != http.StatusBadRequest {
		t.Fatalf("no id: %d, want 400", code)
	}
}

func TestAddressSearchOff(t *testing.T) {
	h := newHarness(t)
	// Same database and session, but no Places client configured.
	off := httptest.NewServer(New(Deps{Config: h.cfg, Store: h.store, Authors: authors.Static{}, Warehouse: h.wh}))
	defer off.Close()
	h.srv = off
	if code := h.do(h.client(userEmail), "GET", "/api/address/suggest?q=15+falls", nil, nil); code != http.StatusNotFound {
		t.Fatalf("off: %d, want 404", code)
	}
}
