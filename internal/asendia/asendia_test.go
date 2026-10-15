package asendia

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDelivered(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/main.js":
			fmt.Fprintf(w, `const b=JSON.parse('{\n    "baseUrl": "%s",\n    "authorizationHeaderValue": "Basic abc",\n    "a1ApiKeyHeaderValue": "KEY-1"\n}');`+
				`const v=(()=>{const e=window.location.href,a="00000000-1111-2222-3333-444444444444";})`, srv.URL)
		case "/api/A1/TrackingBranded/Tracking":
			if r.Header.Get("Authorization") != "Basic abc" || r.Header.Get("X-AsendiaOne-ApiKey") != "KEY-1" ||
				r.URL.Query().Get("trackingKey") != "00000000-1111-2222-3333-444444444444" {
				http.Error(w, "no", http.StatusUnauthorized)
				return
			}
			switch r.URL.Query().Get("trackingNumber") {
			case "AS3": // Canada Post, through the postal network
				fmt.Fprint(w, `{"trackingBrandedDetail":[{"eventCode":"I0","eventDescription":"Delivered","eventOn":"2026-06-15T18:00:00+00:00"},
					{"eventCode":"DG","eventDescription":"Out for Delivery","eventOn":"2026-06-15T09:00:00+00:00"}]}`)
				return
			case "AS4": // Australia Post
				fmt.Fprint(w, `{"trackingBrandedDetail":[{"eventCode":"XX","eventDescription":"Parcel delivered","eventOn":"2026-06-17T03:00:00+00:00"},
					{"eventCode":"FP05","eventDescription":"Parcel out for final mile carrier delivery","eventOn":"2026-06-16T03:00:00+00:00"}]}`)
				return
			}
			if r.URL.Query().Get("trackingNumber") == "AS2" {
				fmt.Fprint(w, `{"trackingBrandedDetail":[{"eventCode":"ARRDEST","eventOn":"2026-09-25T22:16:11+00:00"}]}`)
				return
			}
			fmt.Fprint(w, `{"trackingBrandedDetail":[
				{"eventCode":"DELIVERY","eventOn":"2026-10-06T16:48:00+00:00"},
				{"eventCode":"DELIVERY","eventOn":"2026-10-05T21:06:00+00:00"},
				{"eventCode":"ARRDEST","eventOn":"2026-09-25T22:16:11+00:00"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := &Client{ScriptURL: srv.URL + "/main.js"}
	at, ok, err := c.Delivered(context.Background(), "AS1")
	if err != nil || !ok || at.Format("2006-01-02T15:04") != "2026-10-05T21:06" {
		t.Fatalf("AS1: %v %v %v", at, ok, err)
	}
	for tn, want := range map[string]string{"AS3": "2026-06-15", "AS4": "2026-06-17"} {
		if at, ok, err := c.Delivered(context.Background(), tn); err != nil || !ok || at.Format("2006-01-02") != want {
			t.Errorf("%s: %v %v %v, want %s", tn, at, ok, err, want)
		}
	}
	if _, ok, err := c.Delivered(context.Background(), "AS2"); err != nil || ok {
		t.Fatalf("AS2 isn't delivered: %v %v", ok, err)
	}
}
