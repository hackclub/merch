package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/hackclub/merch/internal/store"
	"github.com/hackclub/merch/internal/swag"
)

// What admins do is evidence too: how long requests wait to ship, what gets
// changed or turned down, and when the catalog and limits changed (so a
// later change in how people behave can be lined up with its cause).
func TestAdminActionsAreTracked(t *testing.T) {
	h := newHarness(t)
	it := h.mkItem("Sti/A", true)
	user, admin := h.client(userEmail), h.client(adminEmail)

	var r store.Request
	if code := h.do(user, "POST", "/api/requests", map[string]any{"purpose": "personal", "address": usAddr, "lines": []swag.Line{{ItemID: it.ID, Quantity: 3}}}, &r); code != 201 {
		t.Fatalf("create = %d", code)
	}
	ship := map[string]any{"lines": []map[string]any{{"sku": "Sti/A", "quantity": 2}}, "note": "We ran low, sorry!"}
	if code := h.do(admin, "POST", fmt.Sprintf("/api/admin/requests/%d/dispatch", r.ID), ship, nil); code != 200 {
		t.Fatalf("dispatch = %d", code)
	}
	evs := h.events("request_dispatched")
	if len(evs) != 1 {
		t.Fatalf("request_dispatched events = %d", len(evs))
	}
	p := props(t, evs[0])
	if p["request_id"] != float64(r.ID) || p["country"] != "US" || p["items_requested"] != float64(3) || p["items_shipped"] != float64(2) ||
		p["changed"] != true || p["has_note"] != true || p["purpose"] != "personal" {
		t.Errorf("request_dispatched props = %v", p)
	}
	if _, ok := p["wait_h"].(float64); !ok {
		t.Errorf("request_dispatched has no wait_h: %v", p)
	}
	if evs[0].UserID == nil || *evs[0].UserID != r.UserID {
		t.Errorf("request_dispatched belongs to user %v, want the requester %d", evs[0].UserID, r.UserID)
	}

	h.do(admin, "POST", fmt.Sprintf("/api/admin/requests/%d/refresh", r.ID), map[string]any{}, nil)
	if evs := h.events("tracking_updated"); len(evs) != 1 || props(t, evs[0])["has_tracking"] != true {
		t.Errorf("tracking_updated = %+v", evs)
	}

	// International: marked paid by hand, then a second request turned down.
	var intl store.Request
	h.do(h.client("b@x.com"), "POST", "/api/requests", map[string]any{"purpose": "personal", "customs_ok": true, "address": deAddr, "lines": []swag.Line{{ItemID: it.ID, Quantity: 1}}}, &intl)
	h.do(admin, "POST", fmt.Sprintf("/api/admin/requests/%d/mark-paid", intl.ID), map[string]any{}, nil)
	if evs := h.events("request_paid"); len(evs) != 1 || props(t, evs[0])["by"] != "admin" || props(t, evs[0])["country"] != "DE" {
		t.Errorf("request_paid = %+v", evs)
	}
	if code := h.do(admin, "POST", fmt.Sprintf("/api/admin/requests/%d/reject", intl.ID), map[string]any{"note": "Out of stock"}, nil); code != 200 {
		t.Fatalf("reject = %d", code)
	}
	evs = h.events("request_rejected")
	if len(evs) != 1 {
		t.Fatalf("request_rejected events = %d", len(evs))
	}
	if p := props(t, evs[0]); p["from"] != "pending" || p["has_note"] != true || p["country"] != "DE" {
		t.Errorf("request_rejected props = %v", p)
	}

	// Notes to requesters are theirs to read, not ours to log.
	for _, e := range h.events("") {
		if strings.Contains(string(e.Props), "ran low") || strings.Contains(string(e.Props), "Out of stock") {
			t.Errorf("%s logged an admin note: %s", e.Name, e.Props)
		}
	}
}

func TestCatalogAndSettingsChangesAreTracked(t *testing.T) {
	h := newHarness(t)
	admin := h.client(adminEmail)

	var it swag.Item
	if code := h.do(admin, "POST", "/api/admin/items", swag.Item{SKU: "Pri/X", Name: "Poster", MaxPerRequest: 2}, &it); code != 201 {
		t.Fatalf("create item = %d", code)
	}
	if evs := h.events("item_created"); len(evs) != 1 || props(t, evs[0])["item_id"] != float64(it.ID) || props(t, evs[0])["visible"] != false {
		t.Errorf("item_created = %+v", evs)
	}
	it.Visible, it.MaxPerRequest = true, 5
	if code := h.do(admin, "PUT", fmt.Sprintf("/api/admin/items/%d", it.ID), it, nil); code != 200 {
		t.Fatalf("update item = %d", code)
	}
	evs := h.events("item_updated")
	if len(evs) != 1 {
		t.Fatalf("item_updated events = %d", len(evs))
	}
	p := props(t, evs[0])
	changed, _ := p["changed"].(map[string]any)
	vis, _ := changed["visible"].(map[string]any)
	max, _ := changed["max_per_request"].(map[string]any)
	if len(changed) != 2 || vis["from"] != false || vis["to"] != true || max["from"] != float64(2) || max["to"] != float64(5) || p["sku"] != "Pri/X" {
		t.Errorf("item_updated props = %v", p)
	}

	var st swag.Settings
	h.do(admin, "GET", "/api/admin/settings", nil, &st)
	st.RequestCooldownDays += 7
	if code := h.do(admin, "PUT", "/api/admin/settings", st, nil); code != 200 {
		t.Fatalf("settings = %d", code)
	}
	evs = h.events("settings_updated")
	if len(evs) != 1 {
		t.Fatalf("settings_updated events = %d", len(evs))
	}
	changed, _ = props(t, evs[0])["changed"].(map[string]any)
	if cd, _ := changed["request_cooldown_days"].(map[string]any); len(changed) != 1 || cd["to"] != float64(st.RequestCooldownDays) {
		t.Errorf("settings_updated props = %v", props(t, evs[0]))
	}

	var sub store.Submission
	h.do(h.client(authorEmail), "POST", "/api/author/submissions", map[string]any{"program": "Sprig", "title": "Sprig sticker", "kind": "sticker", "file_url": "https://cdn.hackclub.com/sprig.png", "quantity": 500}, &sub)
	h.do(admin, "PUT", fmt.Sprintf("/api/admin/submissions/%d", sub.ID), map[string]any{"status": "approved", "admin_note": "nice"}, nil)
	if evs := h.events("submission_updated"); len(evs) != 1 || props(t, evs[0])["from"] != "submitted" || props(t, evs[0])["to"] != "approved" {
		t.Errorf("submission_updated = %+v", evs)
	}
}

// Every event says which build of the site it came from, so an agent can
// compare how people behaved before and after a change shipped.
func TestEventsCarryTheRelease(t *testing.T) {
	h := newHarness(t, func(d *Deps) {
		d.Static = fstest.MapFS{
			"index.html":           {Data: []byte("<html></html>")},
			"_app/version.json":    {Data: []byte(`{"version":"20261017T141645Z"}`)},
			"manifest.webmanifest": {Data: []byte(`{}`)},
		}
	})
	c := h.visitorClient(userEmail, "s1", "/")
	h.do(c, "GET", "/api/me", nil, nil)
	body := map[string]any{"visit": "s1", "release": "20261016T090000Z", "events": []map[string]any{{"name": "page_view", "path": "/"}}}
	if code := h.do(c, "POST", "/api/events", body, nil); code != 204 {
		t.Fatalf("events = %d", code)
	}
	if evs := h.events("api"); len(evs) == 0 || evs[0].Release != "20261017T141645Z" {
		t.Errorf("server event release = %+v", evs)
	}
	// The page reports the build it's running (an open tab can be older than the server).
	if evs := h.events("page_view"); len(evs) != 1 || evs[0].Release != "20261016T090000Z" {
		t.Errorf("client event release = %+v", evs)
	}
	body["release"] = "<script>"
	h.do(c, "POST", "/api/events", body, nil)
	if evs := h.events("page_view"); evs[1].Release != "" {
		t.Errorf("bad release kept: %q", evs[1].Release)
	}
}

// A 500 says what broke on its api event, so errors can be found without the pod's logs.
func TestServerErrorsAreLogged(t *testing.T) {
	h := newHarness(t)
	s := &server{Deps: Deps{Store: h.store, Log: slog.New(slog.DiscardHandler)}}
	handler := s.logAPI(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.serverErr(w, fmt.Errorf("load items: %w", errors.New("connection refused")))
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), "GET", "/api/catalog", nil))
	evs := h.events("api")
	if rec.Code != 500 || len(evs) != 1 {
		t.Fatalf("code %d, %d api events", rec.Code, len(evs))
	}
	if p := props(t, evs[0]); p["status"] != float64(500) || p["error"] != "load items: connection refused" {
		t.Errorf("api props = %v", p)
	}
}

// Admins keep Markdown notes on a request for each other. Only admins can
// write or read them, and the event says a note changed, never what it says.
func TestAdminInternalNote(t *testing.T) {
	h := newHarness(t)
	it := h.mkItem("Sti/A", true)
	user, admin := h.client(userEmail), h.client(adminEmail)
	var r store.Request
	if code := h.do(user, "POST", "/api/requests", map[string]any{"purpose": "personal", "address": usAddr, "lines": []swag.Line{{ItemID: it.ID, Quantity: 1}}}, &r); code != 201 {
		t.Fatalf("create = %d", code)
	}
	path := fmt.Sprintf("/api/admin/requests/%d/internal-note", r.ID)
	note := "**Asked on Slack** for a second pack; see [thread](https://example.com/t)"
	if code := h.do(user, "PUT", path, map[string]any{"internal_note": note}, nil); code != 403 {
		t.Errorf("requester set internal note: %d", code)
	}
	var got map[string]any
	if code := h.do(admin, "PUT", path, map[string]any{"internal_note": note + "\n"}, &got); code != 200 || got["internal_note"] != note {
		t.Fatalf("set internal note = %d %v", code, got)
	}
	var all struct{ Requests []map[string]any }
	h.do(admin, "GET", "/api/admin/requests", nil, &all)
	if all.Requests[0]["internal_note"] != note {
		t.Errorf("admin list internal_note = %v", all.Requests[0]["internal_note"])
	}
	var mine struct{ Requests []map[string]any }
	h.do(user, "GET", "/api/requests", nil, &mine)
	if _, ok := mine.Requests[0]["internal_note"]; ok {
		t.Errorf("requester can see internal_note: %v", mine.Requests[0])
	}
	if code := h.do(admin, "PUT", "/api/admin/requests/999999/internal-note", map[string]any{"internal_note": "x"}, nil); code != 404 {
		t.Errorf("missing request = %d", code)
	}

	evs := h.events("internal_note_updated")
	if len(evs) != 1 {
		t.Fatalf("internal_note_updated events = %d", len(evs))
	}
	p := props(t, evs[0])
	if p["request_id"] != float64(r.ID) || p["has_note"] != true || p["chars"] != float64(len(note)) || p["from_chars"] != float64(0) {
		t.Errorf("internal_note_updated props = %v", p)
	}
	if strings.Contains(string(evs[0].Props), "Slack") {
		t.Errorf("internal_note_updated logged the note: %s", evs[0].Props)
	}
}
