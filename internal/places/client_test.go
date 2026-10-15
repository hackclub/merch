package places

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hackclub/merch/internal/swag"
)

func TestSuggest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/places:autocomplete" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-Goog-Api-Key") != "key" {
			t.Errorf("api key header = %q", r.Header.Get("X-Goog-Api-Key"))
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["input"] != "15 falls" || body["sessionToken"] != "sess" {
			t.Errorf("body = %v", body)
		}
		w.Write([]byte(`{"suggestions":[
			{"placePrediction":{"placeId":"abc","text":{"text":"15 Falls Rd, Shelburne, VT, USA"},
			 "structuredFormat":{"mainText":{"text":"15 Falls Rd"},"secondaryText":{"text":"Shelburne, VT, USA"}}}},
			{"queryPrediction":{"text":{"text":"falls"}}}
		]}`))
	}))
	defer srv.Close()

	got, err := New(srv.URL, "key", nil).Suggest(context.Background(), "15 falls", "sess", Near{})
	if err != nil {
		t.Fatal(err)
	}
	want := []Suggestion{{PlaceID: "abc", Main: "15 Falls Rd", Secondary: "Shelburne, VT, USA"}}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestLookupUS(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/places/abc" || r.URL.Query().Get("sessionToken") != "sess" {
			t.Errorf("got %s", r.URL)
		}
		if r.Header.Get("X-Goog-FieldMask") == "" {
			t.Error("no field mask")
		}
		w.Write([]byte(`{"addressComponents":[
			{"longText":"15","shortText":"15","types":["street_number"]},
			{"longText":"Falls Road","shortText":"Falls Rd","types":["route"]},
			{"longText":"Apt 2","shortText":"2","types":["subpremise"]},
			{"longText":"Shelburne","shortText":"Shelburne","types":["locality","political"]},
			{"longText":"Vermont","shortText":"VT","types":["administrative_area_level_1","political"]},
			{"longText":"United States","shortText":"US","types":["country","political"]},
			{"longText":"05482","shortText":"05482","types":["postal_code"]}
		],"postalAddress":{"regionCode":"US","postalCode":"05482","administrativeArea":"Vermont","locality":"Shelburne","addressLines":["15 Falls Rd"]}}`))
	}))
	defer srv.Close()

	got, err := New(srv.URL, "key", nil).Lookup(context.Background(), "abc", "sess")
	if err != nil {
		t.Fatal(err)
	}
	want := swag.Address{Line1: "15 Falls Rd", Line2: "Apt 2", City: "Shelburne", State: "VT", PostalCode: "05482", Country: "US"}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// Without postalAddress we build the street line from components, in the
// country's order (number after the street in Germany).
func TestLookupFromComponents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"addressComponents":[
			{"longText":"5","shortText":"5","types":["street_number"]},
			{"longText":"Hauptstraße","shortText":"Hauptstraße","types":["route"]},
			{"longText":"Berlin","shortText":"Berlin","types":["locality","political"]},
			{"longText":"Berlin","shortText":"BE","types":["administrative_area_level_1","political"]},
			{"longText":"Germany","shortText":"DE","types":["country","political"]},
			{"longText":"10115","shortText":"10115","types":["postal_code"]}
		]}`))
	}))
	defer srv.Close()

	got, err := New(srv.URL, "key", nil).Lookup(context.Background(), "x", "")
	if err != nil {
		t.Fatal(err)
	}
	want := swag.Address{Line1: "Hauptstraße 5", City: "Berlin", State: "BE", PostalCode: "10115", Country: "DE"}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// UK addresses name the town as postal_town, not locality.
func TestLookupPostalTown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"addressComponents":[
			{"longText":"10","shortText":"10","types":["street_number"]},
			{"longText":"Downing Street","shortText":"Downing St","types":["route"]},
			{"longText":"London","shortText":"London","types":["postal_town"]},
			{"longText":"England","shortText":"England","types":["administrative_area_level_1","political"]},
			{"longText":"United Kingdom","shortText":"GB","types":["country","political"]},
			{"longText":"SW1A 2AA","shortText":"SW1A 2AA","types":["postal_code"]}
		]}`))
	}))
	defer srv.Close()

	got, err := New(srv.URL, "key", nil).Lookup(context.Background(), "x", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Line1 != "10 Downing St" || got.City != "London" || got.Country != "GB" {
		t.Fatalf("got %+v", got)
	}
}

func TestErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":{"message":"API key not valid"}}`))
	}))
	defer srv.Close()
	if _, err := New(srv.URL, "key", nil).Suggest(context.Background(), "x", "", Near{}); err == nil {
		t.Fatal("want error")
	}
}

func TestSuggestNear(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body = nil
		json.NewDecoder(r.Body).Decode(&body)
		w.Write([]byte(`{"suggestions":[]}`))
	}))
	defer srv.Close()
	c := New(srv.URL, "key", nil)

	if _, err := c.Suggest(context.Background(), "15 falls", "", Near{Lat: 44.38, Lng: -73.22, Region: "US"}); err != nil {
		t.Fatal(err)
	}
	circle, _ := body["locationBias"].(map[string]any)["circle"].(map[string]any)
	center, _ := circle["center"].(map[string]any)
	if center["latitude"] != 44.38 || center["longitude"] != -73.22 || circle["radius"] != 50000.0 || body["regionCode"] != "US" {
		t.Fatalf("near the person: body = %v", body)
	}

	c.Suggest(context.Background(), "15 falls", "", Near{Region: "GB"})
	if _, ok := body["locationBias"]; ok || body["regionCode"] != "GB" {
		t.Fatalf("region only: body = %v", body)
	}
	c.Suggest(context.Background(), "15 falls", "", Near{})
	if _, ok := body["locationBias"]; ok || body["regionCode"] != nil {
		t.Fatalf("no hint: body = %v", body)
	}
}

func TestLocate(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Method != http.MethodPost || r.URL.Path != "/v1/places:searchText" || r.Header.Get("X-Goog-FieldMask") != "places.location" {
			t.Errorf("got %s %s mask %q", r.Method, r.URL.Path, r.Header.Get("X-Goog-FieldMask"))
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["textQuery"] == "Shelburne, VT 05482, US" {
			w.Write([]byte(`{"places":[{"location":{"latitude":44.38,"longitude":-73.22}}]}`))
		} else {
			w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()
	c := New(srv.URL, "key", nil)

	lat, lng, ok, err := c.Locate(context.Background(), "Shelburne, VT 05482, US")
	if err != nil || !ok || lat != 44.38 || lng != -73.22 {
		t.Fatalf("Locate = %v %v %v %v", lat, lng, ok, err)
	}
	// Remembered: one billed lookup per address.
	c.Locate(context.Background(), "Shelburne, VT 05482, US")
	if hits != 1 {
		t.Fatalf("hits = %d, want 1 (cached)", hits)
	}
	if _, _, ok, err := c.Locate(context.Background(), "Nowhere"); ok || err != nil {
		t.Fatalf("no match: ok=%v err=%v", ok, err)
	}
}
