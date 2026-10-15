package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hackclub/merch/internal/store"
	"github.com/hackclub/merch/internal/swag"
)

// events returns what's been recorded under name, oldest first.
func (h *harness) events(name string) []store.Event {
	h.t.Helper()
	evs, err := h.store.RecentEvents(context.Background(), name, 100)
	if err != nil {
		h.t.Fatal(err)
	}
	for i, j := 0, len(evs)-1; i < j; i, j = i+1, j-1 {
		evs[i], evs[j] = evs[j], evs[i]
	}
	return evs
}

func props(t *testing.T, e store.Event) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(e.Props, &m); err != nil {
		t.Fatalf("%s props %s: %v", e.Name, e.Props, err)
	}
	return m
}

// visitorClient keeps cookies, like a browser, and sends a visit id and the
// page it's on with every call.
func (h *harness) visitorClient(email, visit, page string) *http.Client {
	c := h.client(email)
	c.Jar = newJar()
	base := c.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	c.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		r.Header.Set("X-Merch-Visit", visit)
		r.Header.Set("Referer", h.srv.URL+page)
		return base.RoundTrip(r)
	})
	return c
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestVisitorCookie(t *testing.T) {
	h := newHarness(t)
	c := h.visitorClient("", "s1", "/")
	resp, err := c.Get(h.srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	u, _ := url.Parse(h.srv.URL)
	var first string
	for _, ck := range c.Jar.Cookies(u) {
		if ck.Name == visitorCookie {
			first = ck.Value
		}
	}
	if len(first) < 16 {
		t.Fatalf("no visitor cookie after the first page: %q", first)
	}
	// It sticks: later calls neither replace it nor set it again.
	resp, _ = c.Get(h.srv.URL + "/api/me")
	resp.Body.Close()
	if resp.Header.Get("Set-Cookie") != "" {
		t.Errorf("visitor cookie set again: %s", resp.Header.Get("Set-Cookie"))
	}
}

func TestClientEvents(t *testing.T) {
	h := newHarness(t)
	c := h.visitorClient(userEmail, "s1", "/")
	sent := time.Now().Add(time.Hour).UnixMilli() // the browser's clock is an hour fast
	body := map[string]any{
		"visit": "s1",
		"sent":  sent,
		"events": []map[string]any{
			{"name": "visit_start", "path": "/", "t": sent - 5000, "props": map[string]any{"referrer": "https://hackclub.com/"}},
			{"name": "page_view", "path": "/items/3-sticker", "t": sent - 1000},
			{"name": "Bad Name!", "path": "/", "t": sent},                                     // dropped
			{"name": "click", "path": "/", "t": sent, "props": "not an object"},               // dropped
			{"name": "click", "path": "/", "t": sent, "props": map[string]any{"text": "Add"}}, // kept
		},
	}
	if code := h.do(c, "POST", "/api/events", body, nil); code != http.StatusNoContent {
		t.Fatalf("events = %d", code)
	}
	evs := h.events("")
	if len(evs) != 3 {
		t.Fatalf("got %d events: %+v", len(evs), evs)
	}
	start, view := evs[0], evs[1]
	if start.Name != "visit_start" || start.Source != "client" || start.Visit != "s1" || start.Visitor == "" || start.UserID == nil {
		t.Errorf("visit_start = %+v", start)
	}
	// Times are moved onto the server's clock, keeping their spacing.
	if d := time.Since(start.At); d < 4*time.Second || d > 10*time.Second {
		t.Errorf("visit_start at %v ago, want ~5s", d)
	}
	if p := props(t, start); p["referrer"] != "https://hackclub.com/" || !strings.Contains(p["ua"].(string), "Go-http-client") {
		t.Errorf("visit_start props = %v", p)
	}
	if view.Path != "/items/3-sticker" || view.Visitor != start.Visitor {
		t.Errorf("page_view = %+v", view)
	}

	// Anyone can send them; anonymous events have no user.
	anon := h.visitorClient("", "s2", "/")
	if code := h.do(anon, "POST", "/api/events", map[string]any{"visit": "s2", "events": []map[string]any{{"name": "page_view", "path": "/"}}}, nil); code != http.StatusNoContent {
		t.Fatalf("anon events = %d", code)
	}
	if e := h.events("page_view"); len(e) != 2 || e[1].UserID != nil || e[1].Visit != "s2" {
		t.Errorf("anon page_view = %+v", e)
	}

	// Batches are bounded.
	many := make([]map[string]any, 201)
	for i := range many {
		many[i] = map[string]any{"name": "click"}
	}
	if code := h.do(anon, "POST", "/api/events", map[string]any{"events": many}, nil); code != http.StatusRequestEntityTooLarge {
		t.Errorf("201 events = %d", code)
	}
}

func TestEventsAreJSONOnly(t *testing.T) {
	h := newHarness(t)
	resp, err := http.Post(h.srv.URL+"/api/events", "text/plain", bytes.NewReader([]byte(`{"events":[]}`)))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain events = %d", resp.StatusCode)
	}
}

func TestShippingEstimateIsLogged(t *testing.T) {
	h := newHarness(t)
	poster, postcard, magazine := shippingFixture(t, h)
	c := h.visitorClient(userEmail, "s1", "/#request")
	body := map[string]any{"country": "de", "lines": []swag.Line{{ItemID: magazine.ID, Quantity: 1}}}
	if code := h.do(c, "POST", "/api/shipping/estimate", body, nil); code != 200 {
		t.Fatalf("estimate = %d", code)
	}
	// One that won't pack is logged with why.
	body = map[string]any{"country": "US", "lines": []swag.Line{{ItemID: poster.ID, Quantity: 1}, {ItemID: postcard.ID, Quantity: 50}}}
	if code := h.do(c, "POST", "/api/shipping/estimate", body, nil); code != 422 {
		t.Fatalf("won't pack = %d", code)
	}
	evs := h.events("shipping_estimate")
	if len(evs) != 2 {
		t.Fatalf("got %d shipping_estimate events", len(evs))
	}
	ok, bad := evs[0], evs[1]
	if ok.Source != "server" || ok.Visit != "s1" || ok.Path != "/" || ok.UserID == nil || ok.Visitor == "" {
		t.Errorf("estimate event = %+v", ok)
	}
	p := props(t, ok)
	lines := p["lines"].([]any)
	if p["country"] != "DE" || p["service"] != "Asendia" || p["package"] != "Bubble mailer" || p["fee_cents"].(float64) <= 0 ||
		p["arrive_by"] == "" || len(lines) != 1 || lines[0].(map[string]any)["item_id"].(float64) != float64(magazine.ID) {
		t.Errorf("estimate props = %v", p)
	}
	if p := props(t, bad); p["error"] == nil || p["country"] != "US" {
		t.Errorf("won't-pack props = %v", p)
	}
}

func TestRequestsAreLogged(t *testing.T) {
	h := newHarness(t)
	it := h.mkItem("Sti/A", true)
	c := h.visitorClient(userEmail, "s1", "/")
	ok := map[string]any{"purpose": "personal", "address": usAddr, "lines": []swag.Line{{ItemID: it.ID, Quantity: 2}}}
	var req store.Request
	if code := h.do(c, "POST", "/api/requests", ok, &req); code != 201 {
		t.Fatalf("create = %d", code)
	}
	tooMany := map[string]any{"purpose": "personal", "address": usAddr, "lines": []swag.Line{{ItemID: it.ID, Quantity: 99}}}
	if code := h.do(c, "POST", "/api/requests", tooMany, nil); code != 422 {
		t.Fatalf("too many = %d", code)
	}
	if code := h.do(c, "POST", "/api/requests/"+strconv.FormatInt(req.ID, 10)+"/cancel", map[string]any{}, nil); code != 200 {
		t.Fatalf("cancel = %d", code)
	}
	created := h.events("request_created")
	if len(created) != 1 {
		t.Fatalf("request_created = %+v", created)
	}
	p := props(t, created[0])
	if p["request_id"].(float64) != float64(req.ID) || p["country"] != "US" || p["purpose"] != "personal" || p["items"].(float64) != 2 || p["status"] != "pending" {
		t.Errorf("request_created props = %v", p)
	}
	// No address in the log, just the country.
	if strings.Contains(string(created[0].Props), "Burlington") {
		t.Errorf("address leaked into the log: %s", created[0].Props)
	}
	refused := h.events("request_refused")
	if len(refused) != 1 || props(t, refused[0])["error"] == nil {
		t.Errorf("request_refused = %+v", refused)
	}
	if c := h.events("request_cancelled"); len(c) != 1 || props(t, c[0])["request_id"].(float64) != float64(req.ID) {
		t.Errorf("request_cancelled = %+v", c)
	}
}

func TestSignInIsLogged(t *testing.T) {
	h := newHarness(t)
	jar := newJar()
	c := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, _ := c.Get(h.srv.URL + "/auth/login")
	resp.Body.Close()
	loc, _ := url.Parse(resp.Header.Get("Location"))
	resp, _ = c.Get(h.srv.URL + "/auth/callback?code=code-user&state=" + url.QueryEscape(loc.Query().Get("state")))
	resp.Body.Close()
	evs := h.events("sign_in")
	// The browser's visitor id ties what they did before signing in to who they are.
	if len(evs) != 1 || evs[0].UserID == nil || evs[0].Visitor == "" {
		t.Fatalf("sign_in = %+v", evs)
	}
	if l := h.events("login_start"); len(l) != 1 || l[0].Visitor != evs[0].Visitor {
		t.Errorf("login_start = %+v", l)
	}
}

func TestAPICallsAreLogged(t *testing.T) {
	h := newHarness(t)
	c := h.visitorClient(userEmail, "s1", "/items/3-x")
	h.do(c, "GET", "/api/catalog", nil, nil)
	h.do(c, "GET", "/api/nope?q=1+Main+St", nil, nil)
	h.do(c, "POST", "/api/events", map[string]any{"events": []map[string]any{{"name": "click"}}}, nil)
	h.do(c, "GET", "/healthz", nil, nil)
	evs := h.events("api")
	if len(evs) != 2 {
		t.Fatalf("api events = %+v", evs)
	}
	cat, nope := props(t, evs[0]), props(t, evs[1])
	if cat["route"] != "GET /api/catalog" || cat["status"].(float64) != 200 || cat["ms"] == nil || evs[0].Path != "/items/3-x" || evs[0].Visit != "s1" {
		t.Errorf("catalog api event = %+v %v", evs[0], cat)
	}
	// The URL path, never the query (address searches carry what people typed).
	if nope["route"] != "/api/" || nope["url"] != "/api/nope" || nope["status"].(float64) != 404 {
		t.Errorf("unknown route api event = %v", nope)
	}
}
