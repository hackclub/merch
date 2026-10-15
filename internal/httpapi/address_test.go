package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/hackclub/merch/internal/auth"
	"github.com/hackclub/merch/internal/authors"
	"github.com/hackclub/merch/internal/places"
	"github.com/hackclub/merch/internal/swag"
)

type fakePlaces struct {
	mu      sync.Mutex
	calls   []string
	near    []places.Near
	located []string
	fail    error
}

func (f *fakePlaces) Suggest(_ context.Context, input, session string, near places.Near) ([]places.Suggestion, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "suggest:"+input+":"+session)
	f.near = append(f.near, near)
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

func (f *fakePlaces) Locate(_ context.Context, text string) (float64, float64, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.located = append(f.located, text)
	return 44.38, -73.22, true, nil
}

func TestAddressSuggestNearThePerson(t *testing.T) {
	h := newHarness(t)
	// No saved address: the browser's region is the only hint.
	h.do(h.client(userEmail), "GET", "/api/address/suggest?q=15+falls&region=gb", nil, nil)
	h.do(h.client(userEmail), "GET", "/api/address/suggest?q=15+falls&region=nope", nil, nil)
	if len(h.places.near) != 2 || h.places.near[0] != (places.Near{Region: "GB"}) || h.places.near[1] != (places.Near{}) {
		t.Fatalf("near = %+v", h.places.near)
	}

	// With a Hack Club Auth address: near it, in its country (only the town is looked up).
	u, err := h.store.UpsertUser(context.Background(), auth.Identity{ID: "ident!home", Email: "home@x.com", Name: "Home",
		Address: &swag.Address{FirstName: "O", LastName: "D", Line1: "15 Falls Rd", City: "Shelburne", State: "VT", PostalCode: "05482", Country: "US"}})
	if err != nil {
		t.Fatal(err)
	}
	tok, _ := h.store.CreateSession(context.Background(), u.ID, time.Hour)
	home := &http.Client{Transport: cookieTransport{tok: tok}}
	h.do(home, "GET", "/api/address/suggest?q=15+falls&region=GB", nil, nil)
	if got := h.places.near[2]; got != (places.Near{Lat: 44.38, Lng: -73.22, Region: "US"}) {
		t.Fatalf("near = %+v", got)
	}
	if len(h.places.located) != 1 || h.places.located[0] != "Shelburne, VT 05482, US" {
		t.Fatalf("located = %v", h.places.located)
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

func TestAddressSuggestWarmsUp(t *testing.T) {
	h := newHarness(t)
	u, err := h.store.UpsertUser(context.Background(), auth.Identity{ID: "ident!warm", Email: "warm@x.com", Name: "Warm",
		Address: &swag.Address{FirstName: "O", LastName: "D", Line1: "15 Falls Rd", City: "Shelburne", State: "VT", PostalCode: "05482", Country: "US"}})
	if err != nil {
		t.Fatal(err)
	}
	tok, _ := h.store.CreateSession(context.Background(), u.ID, time.Hour)
	home := &http.Client{Transport: cookieTransport{tok: tok}}
	// Focusing the field sends an empty query: no billed suggestion, but the
	// person's town is located now instead of in front of their first keystroke.
	if code := h.do(home, "GET", "/api/address/suggest?q=", nil, nil); code != http.StatusOK {
		t.Fatalf("warm: %d", code)
	}
	if len(h.places.calls) != 0 || len(h.places.located) != 1 {
		t.Fatalf("calls = %v, located = %v", h.places.calls, h.places.located)
	}
}
