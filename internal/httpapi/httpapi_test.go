package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"maps"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/hackclub/merch/internal/auth"
	"github.com/hackclub/merch/internal/authors"
	"github.com/hackclub/merch/internal/config"
	"github.com/hackclub/merch/internal/db/dbtest"
	"github.com/hackclub/merch/internal/importer"
	"github.com/hackclub/merch/internal/store"
	"github.com/hackclub/merch/internal/swag"
	"github.com/hackclub/merch/internal/theseus"
)

// ---- fakes ----

type fakeProvider struct{ ids map[string]auth.Identity }

func (f fakeProvider) AuthCodeURL(state, redirectURL string) string {
	return "https://auth.example/oauth/authorize?state=" + url.QueryEscape(state) + "&redirect_uri=" + url.QueryEscape(redirectURL)
}

// Exchange encodes the expected redirect in the code ("code@redirect") to prove
// the callback reuses the redirect_uri the flow started with.
func (f fakeProvider) Exchange(_ context.Context, code, redirectURL string) (*auth.Identity, error) {
	id, ok := f.ids[code]
	if !ok {
		return nil, fmt.Errorf("bad code")
	}
	if want, ok := f.ids[code+"@redirect"]; ok && want.ID != redirectURL {
		return nil, fmt.Errorf("redirect_uri mismatch: %s", redirectURL)
	}
	return &id, nil
}

type fakeWarehouse struct {
	mu       sync.Mutex
	orders   []theseus.OrderInput
	fail     error
	skuCalls int
	skuFail  error
	gets     int
}

func (f *fakeWarehouse) ListSKUs(context.Context) ([]theseus.SKU, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.skuCalls++
	if f.skuFail != nil {
		return nil, f.skuFail
	}
	return []theseus.SKU{
		{SKU: "Sti/A", Name: "Sticker A", InStock: 100, Enabled: true, UnitCost: "0.25"},
		{SKU: "Sti/AB", Name: "Sticker AB", InStock: 7, Enabled: true, UnitCost: "9"},
		{SKU: "Swa/Shirt/HC/S", Name: "Shirt S", InStock: 92, Enabled: true, UnitCost: "13.967"},
		{SKU: "Swa/Shirt/HC/M", Name: "Shirt M", InStock: 95, Enabled: true, UnitCost: "13.8166"},
		{SKU: "Swa/Shirt/HC/XL", Name: "Shirt XL", InStock: 122, Enabled: true, UnitCost: "14.2682"},
		{SKU: "Swa/Hood/Small", Name: "Hoodie S", InStock: 4, Enabled: true, UnitCost: "30"},
		{SKU: "Pri/Hav/4x6/1st", Name: "Haven Postcard", InStock: -503, Inbound: &havenInbound, Enabled: true, UnitCost: "0.05"},
	}, nil
}

// havenInbound is the Haven postcard's restock: backordered by 503 with
// 15,000 on the way, as on 2026-10-17.
var havenInbound = 15000

func (f *fakeWarehouse) CreateOrder(_ context.Context, in theseus.OrderInput) (*theseus.Order, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, f.fail
	}
	f.orders = append(f.orders, in)
	// Theseus charges contents and labor when it takes the order.
	return &theseus.Order{ID: fmt.Sprintf("pkg!%d", len(f.orders)), Status: "dispatched", ContentsCost: "0.50", LaborCost: 2.2}, nil
}

// GetOrder: every order has been mailed, for $4.71.
func (f *fakeWarehouse) GetOrder(_ context.Context, id string) (*theseus.Order, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gets++
	mailed := time.Date(2026, 10, 17, 20, 0, 0, 0, time.UTC)
	return &theseus.Order{ID: id, Status: "mailed", TrackingNumber: "9400", Carrier: "USPS", MailedAt: &mailed,
		ContentsCost: "0.50", LaborCost: 2.2, PostageCost: "4.71"}, nil
}

type fakeCDN struct {
	mu      sync.Mutex
	uploads []fakeUpload
}

type fakeUpload struct {
	name, contentType string
	data              []byte
}

func (f *fakeCDN) Upload(_ context.Context, name, contentType string, data []byte) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uploads = append(f.uploads, fakeUpload{name, contentType, data})
	return fmt.Sprintf("https://cdn.example/%d/%s", len(f.uploads), name), nil
}

// ---- harness ----

type harness struct {
	t      *testing.T
	srv    *httptest.Server
	store  *store.Store
	wh     *fakeWarehouse
	cdn    *fakeCDN
	places *fakePlaces
	stripe *fakeStripe // the default payments, unless an option swaps them
	cfg    *config.Config
}

const (
	adminEmail  = "zach@hackclub.com"
	authorEmail = "author@hackclub.com"
	userEmail   = "hacker@example.com"
)

func newHarness(t *testing.T, opts ...func(*Deps)) *harness {
	t.Helper()
	st := store.New(dbtest.New(t))
	cfg := &config.Config{
		BaseURL:              "http://app.test",
		ExtraBaseURLs:        []string{"http://porygon:5173"},
		AdminEmails:          []string{adminEmail},
		TheseusOrderTag:      "merch",
		StripePublishableKey: "pk_test_x",
	}
	fs := &fakeStripe{}
	wh := &fakeWarehouse{}
	cdn := &fakeCDN{}
	pl := &fakePlaces{}
	d := Deps{
		Config: cfg,
		Store:  st,
		Provider: fakeProvider{ids: map[string]auth.Identity{
			"code-user": {ID: "ident!user", Email: userEmail, Name: "Hacker"},
			"code-porygon": {ID: "ident!p", Email: "p@example.com", Name: "P",
				Address: &swag.Address{FirstName: "P", LastName: "Q", Line1: "15 Falls Rd", City: "Shelburne", State: "VT", PostalCode: "05482", Country: "US"}},
			"code-porygon@redirect": {ID: "http://porygon:5173/auth/callback"},
		}},
		Authors:   authors.Static{authorEmail: true},
		Warehouse: wh,
		Images:    cdn,
		Places:    pl,
		Payments:  fs,
		Static: fstest.MapFS{
			"index.html":           {Data: []byte("<html>app shell</html>")},
			"manifest.webmanifest": {Data: []byte(`{"name":"x"}`)},
		},
		Now: time.Now,
	}
	for _, o := range opts {
		o(&d)
	}
	srv := httptest.NewServer(New(d))
	t.Cleanup(srv.Close)
	return &harness{t: t, srv: srv, store: st, wh: wh, cdn: cdn, places: pl, stripe: fs, cfg: cfg}
}

// client returns an http.Client logged in as email ("" = anonymous).
func (h *harness) client(email string) *http.Client {
	h.t.Helper()
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if email == "" {
		return c
	}
	u, err := h.store.UpsertUser(context.Background(), auth.Identity{ID: "ident!" + email, Email: email, Name: email})
	if err != nil {
		h.t.Fatal(err)
	}
	tok, err := h.store.CreateSession(context.Background(), u.ID, time.Hour)
	if err != nil {
		h.t.Fatal(err)
	}
	c.Transport = cookieTransport{tok: tok}
	return c
}

type cookieTransport struct{ tok string }

func (c cookieTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: c.tok})
	return http.DefaultTransport.RoundTrip(r)
}

func (h *harness) do(c *http.Client, method, path string, body any, out any) int {
	h.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, h.srv.URL+path, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			h.t.Fatalf("%s %s: decode %q: %v", method, path, data, err)
		}
	}
	return resp.StatusCode
}

func (h *harness) mkItem(sku string, visible bool) swag.Item {
	h.t.Helper()
	it, err := h.store.CreateItem(context.Background(), swag.Item{SKU: sku, Name: sku, Visible: visible, MaxPerRequest: 3})
	if err != nil {
		h.t.Fatal(err)
	}
	return it
}

var usAddr = swag.Address{FirstName: "O", LastName: "D", Line1: "1 Main", City: "Burlington", State: "VT", PostalCode: "05401", Country: "US"}
var deAddr = swag.Address{FirstName: "O", LastName: "D", Line1: "Str 1", City: "Berlin", PostalCode: "10115", Country: "DE", Phone: "+49301234567"}

// ---- tests ----

func TestHealthz(t *testing.T) {
	h := newHarness(t)
	if code := h.do(h.client(""), "GET", "/healthz", nil, nil); code != 200 {
		t.Fatalf("healthz = %d", code)
	}
}

func TestSPAFallbackAndStatic(t *testing.T) {
	h := newHarness(t)
	for _, p := range []string{"/", "/admin", "/requests/12"} {
		resp, _ := http.Get(h.srv.URL + p)
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || !strings.Contains(string(b), "app shell") {
			t.Errorf("%s -> %d %q", p, resp.StatusCode, b)
		}
	}
	resp, _ := http.Get(h.srv.URL + "/manifest.webmanifest")
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("manifest -> %d", resp.StatusCode)
	}
	resp, _ = http.Get(h.srv.URL + "/api/nope")
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Errorf("unknown api path should 404, got %d", resp.StatusCode)
	}
}

func TestLoginFlow(t *testing.T) {
	h := newHarness(t)
	jar := newJar()
	c := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	resp, err := c.Get(h.srv.URL + "/auth/login")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	loc, _ := url.Parse(resp.Header.Get("Location"))
	state := loc.Query().Get("state")
	if resp.StatusCode != http.StatusFound || state == "" {
		t.Fatalf("login -> %d %s", resp.StatusCode, loc)
	}

	// Wrong state is refused.
	resp, _ = c.Get(h.srv.URL + "/auth/callback?code=code-user&state=forged")
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("forged state -> %d", resp.StatusCode)
	}

	resp, _ = c.Get(h.srv.URL + "/auth/callback?code=code-user&state=" + url.QueryEscape(state))
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "/" {
		t.Fatalf("callback -> %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}

	var me meResponse
	h.do(c, "GET", "/api/me", nil, &me)
	if me.User == nil || me.User.Email != userEmail || me.Roles.Admin || me.Roles.Author {
		t.Fatalf("me = %+v", me)
	}

	req, _ := http.NewRequest("POST", h.srv.URL+"/auth/logout", nil)
	resp, _ = c.Do(req)
	resp.Body.Close()
	me = meResponse{}
	h.do(c, "GET", "/api/me", nil, &me)
	if me.User != nil {
		t.Fatalf("still logged in after logout: %+v", me)
	}
}

func TestRoles(t *testing.T) {
	h := newHarness(t)
	cases := map[string]roles{
		"":          {},
		userEmail:   {},
		authorEmail: {Author: true},
		adminEmail:  {Author: true, Admin: true},
	}
	for email, want := range cases {
		var me meResponse
		h.do(h.client(email), "GET", "/api/me", nil, &me)
		if me.Roles != want {
			t.Errorf("%q roles = %+v, want %+v", email, me.Roles, want)
		}
	}
}

func TestAccessControl(t *testing.T) {
	h := newHarness(t)
	anon, user, author, admin := h.client(""), h.client(userEmail), h.client(authorEmail), h.client(adminEmail)
	checks := []struct {
		c    *http.Client
		path string
		want int
	}{
		{anon, "/api/requests", 401},
		{user, "/api/requests", 200},
		{user, "/api/author/submissions", 403},
		{author, "/api/author/submissions", 200},
		{author, "/api/admin/requests", 403},
		{user, "/api/admin/settings", 403},
		{admin, "/api/admin/requests", 200},
		{admin, "/api/admin/settings", 200},
		{admin, "/api/admin/submissions", 200},
		{admin, "/api/admin/items", 200},
		{admin, "/api/admin/warehouse/skus", 200},
	}
	for _, c := range checks {
		if got := h.do(c.c, "GET", c.path, nil, nil); got != c.want {
			t.Errorf("GET %s = %d, want %d", c.path, got, c.want)
		}
	}
}

func TestMutationsRequireJSONAndSameOrigin(t *testing.T) {
	h := newHarness(t)
	c := h.client(userEmail)
	req, _ := http.NewRequest("POST", h.srv.URL+"/api/requests", strings.NewReader("a=b"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, _ := c.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("form post -> %d", resp.StatusCode)
	}
	req, _ = http.NewRequest("POST", h.srv.URL+"/api/requests", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	resp, _ = c.Do(req)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("cross-origin post -> %d", resp.StatusCode)
	}
}

func TestCatalogShowsOnlyVisibleItems(t *testing.T) {
	h := newHarness(t)
	h.mkItem("Sti/A", true)
	h.mkItem("Sti/Hidden", false)
	var out struct {
		Items    []swag.Item `json:"items"`
		Settings struct {
			RequestsOpen                  bool   `json:"requests_open"`
			InternationalShippingFeeCents int    `json:"international_shipping_fee_cents"`
			StripeKey                     string `json:"stripe_key"`
		} `json:"settings"`
	}
	if code := h.do(h.client(""), "GET", "/api/catalog", nil, &out); code != 200 {
		t.Fatalf("catalog = %d", code)
	}
	if len(out.Items) != 1 || out.Items[0].SKU != "Sti/A" {
		t.Fatalf("items = %+v", out.Items)
	}
	if !out.Settings.RequestsOpen || out.Settings.InternationalShippingFeeCents != 1500 || out.Settings.StripeKey != "pk_test_x" {
		t.Fatalf("settings = %+v", out.Settings)
	}
}

func TestItemStockComesFromWarehouse(t *testing.T) {
	h := newHarness(t)
	listed := h.mkItem("Sti/A", true)
	gone := h.mkItem("Sti/Gone", true)
	hidden := h.mkItem("Sti/Hidden", false)
	anon := h.client("")

	type stock struct {
		InStock     int     `json:"in_stock"`
		UnitCost    *string `json:"unit_cost"`
		UnitCostMax *string `json:"unit_cost_max"`
		Variants    []struct {
			SKU      string  `json:"sku"`
			InStock  int     `json:"in_stock"`
			UnitCost *string `json:"unit_cost"`
		} `json:"variants"`
	}
	var got stock
	if code := h.do(anon, "GET", fmt.Sprintf("/api/items/%d/stock", listed.ID), nil, &got); code != 200 ||
		got.InStock != 100 || got.UnitCost == nil || *got.UnitCost != "0.25" || got.UnitCostMax != nil {
		t.Fatalf("listed stock = %d %+v", code, got)
	}
	// A sized item's SKU is the parent of one SKU per size: sum them, and
	// give the unit cost as a range.
	shirt := h.mkItem("Swa/Shirt/HC", true)
	got = stock{}
	if code := h.do(anon, "GET", fmt.Sprintf("/api/items/%d/stock", shirt.ID), nil, &got); code != 200 || got.InStock != 309 ||
		got.UnitCost == nil || *got.UnitCost != "13.8166" || got.UnitCostMax == nil || *got.UnitCostMax != "14.2682" {
		t.Fatalf("sized stock = %d %+v", code, got)
	}
	// Listed sizes count by their own SKUs, and each size gets its count and cost.
	hoodie, err := h.store.CreateItem(context.Background(), swag.Item{SKU: "Swa/Hood", Name: "Hoodie", Visible: true, MaxPerRequest: 1,
		Variants: []swag.Variant{{Label: "S", SKU: "Swa/Hood/Small"}, {Label: "M", SKU: "Swa/Hood/Medium"}}})
	if err != nil {
		t.Fatal(err)
	}
	got = stock{}
	if code := h.do(anon, "GET", fmt.Sprintf("/api/items/%d/stock", hoodie.ID), nil, &got); code != 200 || got.InStock != 4 ||
		len(got.Variants) != 2 || got.Variants[0].SKU != "Swa/Hood/Small" || got.Variants[0].InStock != 4 || got.Variants[1].InStock != 0 ||
		got.Variants[0].UnitCost == nil || *got.Variants[0].UnitCost != "30" || got.Variants[1].UnitCost != nil {
		t.Fatalf("variant stock = %d %+v", code, got)
	}
	// The warehouse only lists in-stock SKUs; a missing one is out of stock.
	got = stock{}
	if code := h.do(anon, "GET", fmt.Sprintf("/api/items/%d/stock", gone.ID), nil, &got); code != 200 || got.InStock != 0 || got.UnitCost != nil {
		t.Fatalf("missing sku stock = %d %+v", code, got)
	}
	if code := h.do(anon, "GET", fmt.Sprintf("/api/items/%d/stock", hidden.ID), nil, nil); code != 404 {
		t.Fatalf("hidden item stock = %d", code)
	}
	if code := h.do(anon, "GET", "/api/items/999999/stock", nil, nil); code != 404 {
		t.Fatalf("unknown item stock = %d", code)
	}
	// One warehouse call serves every product page for a while.
	if h.wh.skuCalls != 1 {
		t.Fatalf("warehouse SKU calls = %d, want 1 (cached)", h.wh.skuCalls)
	}
}

func TestItemStockWarehouseDown(t *testing.T) {
	h := newHarness(t)
	it := h.mkItem("Sti/A", true)
	h.wh.skuFail = errors.New("boom")
	if code := h.do(h.client(""), "GET", fmt.Sprintf("/api/items/%d/stock", it.ID), nil, nil); code != 502 {
		t.Fatalf("stock with warehouse down = %d", code)
	}
}

func TestDomesticRequestFlow(t *testing.T) {
	h := newHarness(t)
	it := h.mkItem("Sti/A", true)
	user, admin := h.client(userEmail), h.client(adminEmail)

	var r store.Request
	code := h.do(user, "POST", "/api/requests", map[string]any{"purpose": "personal", "address": usAddr, "lines": []swag.Line{{ItemID: it.ID, Quantity: 2}}, "note": "thanks!"}, &r)
	if code != 201 || r.Status != swag.StatusPending || r.ShippingFeeCents != 0 {
		t.Fatalf("create = %d %+v", code, r)
	}

	// No cooldown: a second request right away goes through (an agent reviews each one).
	var second store.Request
	if code := h.do(user, "POST", "/api/requests", map[string]any{"purpose": "personal", "address": usAddr, "lines": []swag.Line{{ItemID: it.ID, Quantity: 1}}}, &second); code != 201 {
		t.Fatalf("second request = %d", code)
	}

	var mine struct{ Requests []store.Request }
	h.do(user, "GET", "/api/requests", nil, &mine)
	if len(mine.Requests) != 2 {
		t.Fatalf("mine = %+v", mine)
	}

	var dispatched store.Request
	if code := h.do(admin, "POST", fmt.Sprintf("/api/admin/requests/%d/dispatch", r.ID), map[string]any{}, &dispatched); code != 200 {
		t.Fatalf("dispatch = %d", code)
	}
	if dispatched.Status != swag.StatusDispatched || dispatched.TheseusOrderID != "pkg!1" {
		t.Fatalf("dispatched = %+v", dispatched)
	}
	o := h.wh.orders[0]
	if o.RecipientEmail != userEmail || o.IdempotencyKey != fmt.Sprintf("merch-request-%d", r.ID) || o.Tags[0] != "merch" ||
		o.Contents[0].SKU != "Sti/A" || o.Contents[0].Quantity != 2 || o.Address.Line1 != "1 Main" {
		t.Fatalf("theseus order = %+v", o)
	}

	var tracked store.Request
	h.do(admin, "POST", fmt.Sprintf("/api/admin/requests/%d/refresh", r.ID), map[string]any{}, &tracked)
	if tracked.TrackingNumber != "9400" {
		t.Fatalf("tracked = %+v", tracked)
	}
}

func TestClubOnlyItemsArentForPersonalRequests(t *testing.T) {
	h := newHarness(t)
	admin, user := h.client(adminEmail), h.client(userEmail)
	var mag swag.Item
	if code := h.do(admin, "POST", "/api/admin/items", swag.Item{SKU: "Pri/Bok/M25/1st", Name: "Magazine", MaxPerRequest: 5, Visible: true, ClubOnly: true}, &mag); code != 201 || !mag.ClubOnly {
		t.Fatalf("create club-only item = %d %+v", code, mag)
	}
	lines := []swag.Line{{ItemID: mag.ID, Quantity: 1}}
	var errBody map[string]string
	if code := h.do(user, "POST", "/api/requests", map[string]any{"purpose": "personal", "address": usAddr, "lines": lines}, &errBody); code != 422 || !strings.Contains(errBody["error"], "clubs and events") {
		t.Fatalf("personal = %d %v", code, errBody)
	}
	if code := h.do(user, "POST", "/api/requests", map[string]any{"purpose": "club", "address": usAddr, "lines": lines}, &errBody); code != 422 || !strings.Contains(errBody["error"], "how you'll use it") {
		t.Fatalf("club without a reason = %d %v", code, errBody)
	}
	var r store.Request
	if code := h.do(user, "POST", "/api/requests", map[string]any{"purpose": "club", "note": "Handing them out at our club fair.", "address": usAddr, "lines": lines}, &r); code != 201 || r.Purpose != swag.PurposeClub {
		t.Fatalf("club = %d %+v", code, r)
	}
}

func TestDispatchFailureLeavesRequestPending(t *testing.T) {
	h := newHarness(t)
	it := h.mkItem("Sti/A", true)
	var r store.Request
	h.do(h.client(userEmail), "POST", "/api/requests", map[string]any{"purpose": "personal", "address": usAddr, "lines": []swag.Line{{ItemID: it.ID, Quantity: 1}}}, &r)
	h.wh.fail = fmt.Errorf("theseus down")
	if code := h.do(h.client(adminEmail), "POST", fmt.Sprintf("/api/admin/requests/%d/dispatch", r.ID), map[string]any{}, nil); code != 502 {
		t.Fatalf("dispatch = %d", code)
	}
	got, _ := h.store.GetRequest(context.Background(), r.ID)
	if got.Status != swag.StatusPending {
		t.Fatalf("status = %s", got.Status)
	}
}

func TestInternationalRequestNeedsPayment(t *testing.T) {
	h := newHarness(t)
	it := h.mkItem("Sti/A", true)
	user, admin := h.client(userEmail), h.client(adminEmail)

	var r viewed
	code := h.do(user, "POST", "/api/requests", map[string]any{"purpose": "personal", "customs_ok": true, "address": deAddr, "lines": []swag.Line{{ItemID: it.ID, Quantity: 1}}}, &r)
	if code != 201 || r.Status != swag.StatusAwaitingPayment || r.ShippingFeeCents != 1500 || r.StripeKey != "pk_test_x" {
		t.Fatalf("create = %d %+v", code, r)
	}

	if code := h.do(admin, "POST", fmt.Sprintf("/api/admin/requests/%d/dispatch", r.ID), map[string]any{}, nil); code != 409 {
		t.Fatalf("dispatch unpaid = %d, want 409", code)
	}
	// Only a card payment moves it on: there's no marking it paid by hand.
	if code := h.do(admin, "POST", fmt.Sprintf("/api/admin/requests/%d/mark-paid", r.ID), map[string]any{}, nil); code != 404 && code != 405 {
		t.Fatalf("mark-paid = %d, want gone", code)
	}
	if paid := h.payByCard(userEmail, r.ID); paid.Status != swag.StatusPending || paid.PaidAt == nil {
		t.Fatalf("paid = %+v", paid)
	}
}

func TestUserCanCancelOwnPendingRequestOnly(t *testing.T) {
	h := newHarness(t)
	it := h.mkItem("Sti/A", true)
	var r store.Request
	h.do(h.client(userEmail), "POST", "/api/requests", map[string]any{"purpose": "personal", "address": usAddr, "lines": []swag.Line{{ItemID: it.ID, Quantity: 1}}}, &r)

	if code := h.do(h.client(authorEmail), "POST", fmt.Sprintf("/api/requests/%d/cancel", r.ID), map[string]any{}, nil); code != 404 {
		t.Fatalf("other user cancel = %d", code)
	}
	var got store.Request
	if code := h.do(h.client(userEmail), "POST", fmt.Sprintf("/api/requests/%d/cancel", r.ID), map[string]any{}, &got); code != 200 || got.Status != swag.StatusCancelled {
		t.Fatalf("cancel = %d %+v", code, got)
	}
}

func TestRequestValidation(t *testing.T) {
	h := newHarness(t)
	hidden := h.mkItem("Sti/Hidden", false)
	it := h.mkItem("Sti/A", true)
	user := h.client(userEmail)
	bad := []map[string]any{
		{"address": usAddr, "lines": []swag.Line{}},
		{"address": usAddr, "lines": []swag.Line{{ItemID: hidden.ID, Quantity: 1}}},
		{"address": usAddr, "lines": []swag.Line{{ItemID: it.ID, Quantity: 99}}},
		{"address": swag.Address{Country: "US"}, "lines": []swag.Line{{ItemID: it.ID, Quantity: 1}}},
	}
	for _, b := range bad {
		b["purpose"] = "personal"
	}
	// Every request says what it's for.
	bad = append(bad, map[string]any{"address": usAddr, "lines": []swag.Line{{ItemID: it.ID, Quantity: 1}}},
		map[string]any{"purpose": "resale", "address": usAddr, "lines": []swag.Line{{ItemID: it.ID, Quantity: 1}}})
	for i, b := range bad {
		if code := h.do(user, "POST", "/api/requests", b, nil); code != 422 {
			t.Errorf("case %d = %d, want 422", i, code)
		}
	}
}

func TestAdminManagesItemsAndSettings(t *testing.T) {
	h := newHarness(t)
	admin := h.client(adminEmail)

	var it swag.Item
	if code := h.do(admin, "POST", "/api/admin/items", swag.Item{SKU: "Sti/A", Name: "Sticker", MaxPerRequest: 2, Visible: true}, &it); code != 201 || it.ID == 0 {
		t.Fatalf("create item = %d %+v", code, it)
	}
	if code := h.do(admin, "POST", "/api/admin/items", swag.Item{SKU: "Sti/A", Name: "Dup", MaxPerRequest: 1}, nil); code != 409 {
		t.Fatalf("dup item = %d", code)
	}
	it.Visible = false
	var upd swag.Item
	if code := h.do(admin, "PUT", fmt.Sprintf("/api/admin/items/%d", it.ID), it, &upd); code != 200 || upd.Visible {
		t.Fatalf("update item = %d %+v", code, upd)
	}

	want := swag.Settings{RequestsOpen: false, InternationalShippingFeeCents: 2000}
	var got swag.Settings
	if code := h.do(admin, "PUT", "/api/admin/settings", want, &got); code != 200 || got != want {
		t.Fatalf("settings = %d %+v", code, got)
	}
	if code := h.do(admin, "PUT", "/api/admin/settings", swag.Settings{InternationalShippingFeeCents: -1}, nil); code != 422 {
		t.Fatalf("negative settings = %d", code)
	}

	var skus struct{ SKUs []theseus.SKU }
	h.do(admin, "GET", "/api/admin/warehouse/skus", nil, &skus)
	if len(skus.SKUs) != 7 || skus.SKUs[0].SKU != "Sti/A" {
		t.Fatalf("skus = %+v", skus)
	}
}

func TestSizedItemFlow(t *testing.T) {
	h := newHarness(t)
	admin, user := h.client(adminEmail), h.client(userEmail)
	mOz, noOz := 5.0, 0.0
	sizes := []swag.Variant{{Label: "S", SKU: "Swa/Shirt/HC/S"}, {Label: "M", SKU: "Swa/Shirt/HC/M", WeightOz: &mOz}}

	for i, bad := range [][]swag.Variant{
		{{Label: "S", SKU: "a", WeightOz: &noOz}},
		{{Label: "", SKU: "Swa/Shirt/HC/S"}},
		{{Label: "S", SKU: ""}},
		{{Label: "S", SKU: "a"}, {Label: "S", SKU: "b"}},
		{{Label: "S", SKU: "a"}, {Label: "M", SKU: "a"}},
	} {
		if code := h.do(admin, "POST", "/api/admin/items", swag.Item{SKU: "Swa/Shirt/HC", Name: "T-shirt", MaxPerRequest: 1, Variants: bad}, nil); code != 422 {
			t.Errorf("bad variants %d = %d, want 422", i, code)
		}
	}
	var shirt swag.Item
	if code := h.do(admin, "POST", "/api/admin/items", swag.Item{SKU: "Swa/Shirt/HC", Name: "T-shirt", MaxPerRequest: 1, Visible: true, Variants: sizes}, &shirt); code != 201 {
		t.Fatalf("create sized item = %d", code)
	}
	var errBody map[string]string
	if code := h.do(admin, "POST", "/api/admin/items", swag.Item{SKU: "Swa/Shirt/Two", Name: "x", MaxPerRequest: 1, Variants: sizes[:1]}, &errBody); code != 409 || !strings.Contains(errBody["error"], "Swa/Shirt/HC/S") {
		t.Fatalf("size claimed twice = %d %v", code, errBody)
	}

	var cat struct{ Items []swag.Item }
	h.do(user, "GET", "/api/catalog", nil, &cat)
	if len(cat.Items) != 1 || len(cat.Items[0].Variants) != 2 {
		t.Fatalf("catalog = %+v", cat)
	}
	if w := cat.Items[0].Variants[1].WeightOz; w == nil || *w != 5 {
		t.Fatalf("size weight = %v, want 5 oz", w)
	}

	if code := h.do(user, "POST", "/api/requests", map[string]any{"purpose": "personal", "address": usAddr, "lines": []swag.Line{{ItemID: shirt.ID, Quantity: 1}}}, &errBody); code != 422 || !strings.Contains(errBody["error"], "size") {
		t.Fatalf("no size = %d %v", code, errBody)
	}
	var r store.Request
	if code := h.do(user, "POST", "/api/requests", map[string]any{"purpose": "personal", "address": usAddr, "lines": []swag.Line{{ItemID: shirt.ID, Quantity: 1, SKU: "Swa/Shirt/HC/M"}}}, &r); code != 201 {
		t.Fatalf("create = %d", code)
	}
	if len(r.Lines) != 1 || r.Lines[0].SKU != "Swa/Shirt/HC/M" || r.Lines[0].Variant != "M" {
		t.Fatalf("lines = %+v", r.Lines)
	}
	if code := h.do(admin, "POST", fmt.Sprintf("/api/admin/requests/%d/dispatch", r.ID), map[string]any{}, nil); code != 200 {
		t.Fatalf("dispatch = %d", code)
	}
	if c := h.wh.orders[0].Contents; len(c) != 1 || c[0].SKU != "Swa/Shirt/HC/M" {
		t.Fatalf("theseus contents = %+v, want the chosen size's SKU", c)
	}
}

func TestRequestSeveralSizes(t *testing.T) {
	h := newHarness(t)
	admin, user := h.client(adminEmail), h.client(userEmail)
	sizes := []swag.Variant{{Label: "S", SKU: "Swa/Shirt/HC/S"}, {Label: "M", SKU: "Swa/Shirt/HC/M"}}
	var shirt swag.Item
	if code := h.do(admin, "POST", "/api/admin/items", swag.Item{SKU: "Swa/Shirt/HC", Name: "T-shirt", MaxPerRequest: 3, Visible: true, Variants: sizes}, &shirt); code != 201 {
		t.Fatalf("create sized item = %d", code)
	}
	var r store.Request
	lines := []swag.Line{{ItemID: shirt.ID, Quantity: 1, SKU: "Swa/Shirt/HC/S"}, {ItemID: shirt.ID, Quantity: 2, SKU: "Swa/Shirt/HC/M"}}
	if code := h.do(user, "POST", "/api/requests", map[string]any{"purpose": "personal", "address": usAddr, "lines": lines}, &r); code != 201 {
		t.Fatalf("create = %d", code)
	}
	got := map[string]int{}
	for _, l := range r.Lines {
		got[l.Variant] = l.Quantity
	}
	if len(r.Lines) != 2 || got["S"] != 1 || got["M"] != 2 {
		t.Fatalf("lines = %+v, want 1 S and 2 M", r.Lines)
	}
	if code := h.do(admin, "POST", fmt.Sprintf("/api/admin/requests/%d/dispatch", r.ID), map[string]any{}, nil); code != 200 {
		t.Fatalf("dispatch = %d", code)
	}
	if c := h.wh.orders[0].Contents; len(c) != 2 {
		t.Fatalf("theseus contents = %+v, want one line per size", c)
	}
}

func TestAdminUploadsItemImages(t *testing.T) {
	h := newHarness(t)
	admin := h.client(adminEmail)
	it := h.mkItem("Pri/A", true)
	path := fmt.Sprintf("/api/admin/items/%d/images", it.ID)

	var buf bytes.Buffer
	png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 30, 40)))
	upload := map[string]string{"filename": "Hackpad Poster (front).PNG", "data": base64.StdEncoding.EncodeToString(buf.Bytes())}

	if code := h.do(h.client(userEmail), "POST", path, upload, nil); code != 403 {
		t.Fatalf("non-admin upload = %d", code)
	}
	var upd swag.Item
	if code := h.do(admin, "POST", path, upload, &upd); code != 200 {
		t.Fatalf("upload = %d", code)
	}
	if len(h.cdn.uploads) != 1 {
		t.Fatalf("cdn uploads = %d", len(h.cdn.uploads))
	}
	got := h.cdn.uploads[0]
	if got.contentType != "image/jpeg" || got.name != "hackpad-poster-front.jpg" {
		t.Fatalf("uploaded %q as %s", got.name, got.contentType)
	}
	if _, err := jpeg.Decode(bytes.NewReader(got.data)); err != nil {
		t.Fatalf("uploaded bytes aren't a JPEG: %v", err)
	}
	if want := []string{"https://cdn.example/1/hackpad-poster-front.jpg"}; !slices.Equal(upd.Images, want) {
		t.Fatalf("item images = %v, want %v", upd.Images, want)
	}

	var cat struct{ Items []swag.Item }
	h.do(h.client(""), "GET", "/api/catalog", nil, &cat)
	if len(cat.Items) != 1 || !slices.Equal(cat.Items[0].Images, upd.Images) {
		t.Fatalf("catalog images = %+v", cat.Items)
	}

	bad := map[string]string{"filename": "notes.pdf", "data": base64.StdEncoding.EncodeToString([]byte("%PDF-1.4"))}
	if code := h.do(admin, "POST", path, bad, nil); code != 422 {
		t.Fatalf("non-image upload = %d", code)
	}
	if code := h.do(admin, "POST", "/api/admin/items/999/images", upload, nil); code != 404 {
		t.Fatalf("missing item upload = %d", code)
	}
}

func TestItemImagesMustBeHTTPURLs(t *testing.T) {
	h := newHarness(t)
	it := h.mkItem("Pri/A", true)
	it.Images = []string{"javascript:alert(1)"}
	if code := h.do(h.client(adminEmail), "PUT", fmt.Sprintf("/api/admin/items/%d", it.ID), it, nil); code != 422 {
		t.Fatalf("bad image url = %d", code)
	}
}

func TestItemSizeMustBeWholeAndPlausible(t *testing.T) {
	h := newHarness(t)
	admin := h.client(adminEmail)
	it := h.mkItem("Pri/A", true)
	path := fmt.Sprintf("/api/admin/items/%d", it.ID)
	f := func(v float64) *float64 { return &v }

	for _, bad := range [][2]*float64{{f(11), nil}, {nil, f(17)}, {f(0), f(17)}, {f(11), f(-1)}, {f(500), f(17)}} {
		it.WidthIn, it.HeightIn = bad[0], bad[1]
		if code := h.do(admin, "PUT", path, it, nil); code != 422 {
			t.Errorf("size %v x %v = %d, want 422", bad[0], bad[1], code)
		}
	}
	it.WidthIn, it.HeightIn = f(4), f(6)
	var upd swag.Item
	if code := h.do(admin, "PUT", path, it, &upd); code != 200 || *upd.WidthIn != 4 || *upd.HeightIn != 6 {
		t.Fatalf("good size = %d %+v", code, upd)
	}
	it.WidthIn, it.HeightIn = nil, nil
	if code := h.do(admin, "PUT", path, it, &upd); code != 200 || upd.WidthIn != nil {
		t.Fatalf("clearing size = %d %+v", code, upd)
	}
}

func TestItemPrintDetailsAreValidated(t *testing.T) {
	h := newHarness(t)
	admin := h.client(adminEmail)
	it := h.mkItem("Pri/A", true)
	path := fmt.Sprintf("/api/admin/items/%d", it.ID)
	f := func(v float64) *float64 { return &v }

	for name, bad := range map[string]swag.Item{
		"unknown finish":  {Finish: "velvet"},
		"zero thickness":  {ThicknessIn: f(0)},
		"thick as a book": {ThicknessIn: f(2)},
		"unknown packs":   {Packs: "crate"},
		"photo url":       {Photos: []swag.Photo{{URL: "javascript:alert(1)"}}},
		"long caption":    {Photos: []swag.Photo{{URL: "https://cdn.example/a.jpg", Caption: strings.Repeat("x", 301)}}},
		"print file url":  {PrintFiles: []swag.PrintFile{{URL: "javascript:alert(1)", Label: "Front"}}},
		"long file label": {PrintFiles: []swag.PrintFile{{URL: "https://cdn.example/a.pdf", Label: strings.Repeat("x", 101)}}},
	} {
		bad.SKU, bad.Name, bad.MaxPerRequest = it.SKU, it.Name, it.MaxPerRequest
		if code := h.do(admin, "PUT", path, bad, nil); code != 422 {
			t.Errorf("%s = %d, want 422", name, code)
		}
	}
	it.Finish, it.ThicknessIn, it.Packs = "matte", f(0.008), "sticker"
	var upd swag.Item
	if code := h.do(admin, "PUT", path, it, &upd); code != 200 || upd.Finish != "matte" || *upd.ThicknessIn != 0.008 || upd.Packs != "sticker" {
		t.Fatalf("good print details = %d %+v", code, upd)
	}
	// Enamel pins render as pins in 3D.
	it.Finish, it.Packs = "enamel", ""
	if code := h.do(admin, "PUT", path, it, &upd); code != 200 || upd.Finish != "enamel" {
		t.Fatalf("enamel finish = %d %+v", code, upd)
	}
}

func TestAdminUploadsItemPhotos(t *testing.T) {
	h := newHarness(t)
	it := h.mkItem("Pri/A", true)
	var buf bytes.Buffer
	png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 30, 40)))
	body := map[string]string{"filename": "booth.jpg", "data": base64.StdEncoding.EncodeToString(buf.Bytes()),
		"caption": "  On display at 3DPrintopia ", "credit": "Boa_N"}

	var upd swag.Item
	if code := h.do(h.client(adminEmail), "POST", fmt.Sprintf("/api/admin/items/%d/photos", it.ID), body, &upd); code != 200 {
		t.Fatalf("photo upload = %d", code)
	}
	want := swag.Photo{URL: "https://cdn.example/1/booth.jpg", Caption: "On display at 3DPrintopia", Credit: "Boa_N"}
	if len(upd.Photos) != 1 || upd.Photos[0] != want || len(upd.Images) != 0 {
		t.Fatalf("photos = %+v images = %v", upd.Photos, upd.Images)
	}
	if code := h.do(h.client(userEmail), "POST", fmt.Sprintf("/api/admin/items/%d/photos", it.ID), body, nil); code != 403 {
		t.Fatalf("non-admin photo upload = %d", code)
	}
}

// Print files are the originals people can print themselves, so they go to
// the CDN untouched (not shrunk like gallery images).
func TestAdminUploadsPrintFiles(t *testing.T) {
	h := newHarness(t)
	it := h.mkItem("Pri/A", true)
	path := fmt.Sprintf("/api/admin/items/%d/print-files", it.ID)
	pdf := []byte("%PDF-1.7\n" + strings.Repeat("x", 2000))
	body := map[string]string{"filename": "postcard front.pdf", "data": base64.StdEncoding.EncodeToString(pdf), "label": " Front "}

	var upd swag.Item
	if code := h.do(h.client(adminEmail), "POST", path, body, &upd); code != 200 {
		t.Fatalf("print file upload = %d", code)
	}
	want := swag.PrintFile{URL: "https://cdn.example/1/postcard-front.pdf", Label: "Front", Bytes: int64(len(pdf))}
	if len(upd.PrintFiles) != 1 || upd.PrintFiles[0] != want || len(upd.Images) != 0 {
		t.Fatalf("print files = %+v images = %v", upd.PrintFiles, upd.Images)
	}
	if up := h.cdn.uploads[0]; up.contentType != "application/pdf" || !bytes.Equal(up.data, pdf) {
		t.Fatalf("uploaded %s, %d bytes", up.contentType, len(up.data))
	}
	if e := h.events("item_media_added"); len(e) != 1 || props(t, e[0])["kind"] != "print_file" {
		t.Fatalf("events = %+v", e)
	}
	// The public catalog carries them, for the product page.
	var cat struct{ Items []swag.Item }
	h.do(h.client(userEmail), "GET", "/api/catalog", nil, &cat)
	if len(cat.Items) != 1 || len(cat.Items[0].PrintFiles) != 1 {
		t.Fatalf("catalog print files = %+v", cat.Items)
	}

	body["filename"], body["data"] = "notes.txt", base64.StdEncoding.EncodeToString([]byte("hello"))
	if code := h.do(h.client(adminEmail), "POST", path, body, nil); code != 422 {
		t.Fatalf("text file upload = %d, want 422", code)
	}
	if code := h.do(h.client(userEmail), "POST", path, body, nil); code != 403 {
		t.Fatalf("non-admin print file upload = %d", code)
	}
}

func TestImageUploadsNeedCDNConfigured(t *testing.T) {
	h := newHarness(t)
	h.srv.Config.Handler = New(Deps{Config: h.cfg, Store: h.store, Provider: fakeProvider{}, Authors: authors.Static{}, Warehouse: h.wh})
	it := h.mkItem("Pri/A", true)
	code := h.do(h.client(adminEmail), "POST", fmt.Sprintf("/api/admin/items/%d/images", it.ID), map[string]string{"filename": "a.png", "data": ""}, nil)
	if code != 503 {
		t.Fatalf("upload without CDN = %d", code)
	}
}

func TestAuthorSubmissionFlow(t *testing.T) {
	h := newHarness(t)
	author, admin := h.client(authorEmail), h.client(adminEmail)

	var sub store.Submission
	body := map[string]any{"program": "Sprig", "title": "Sprig sticker", "kind": "sticker", "file_url": "https://cdn.hackclub.com/sprig.png", "quantity": 500}
	if code := h.do(author, "POST", "/api/author/submissions", body, &sub); code != 201 || sub.Status != "submitted" {
		t.Fatalf("submit = %d %+v", code, sub)
	}
	if code := h.do(h.client(userEmail), "POST", "/api/author/submissions", body, nil); code != 403 {
		t.Fatalf("non-author submit = %d", code)
	}
	if code := h.do(author, "POST", "/api/author/submissions", map[string]any{"title": "x", "file_url": "javascript:alert(1)", "quantity": 1, "program": "p"}, nil); code != 422 {
		t.Fatalf("bad url submit = %d", code)
	}
	for k, n := range map[string]int{"title": 201, "program": 201, "kind": 41, "description": 5001, "file_url": 2001} {
		long := maps.Clone(body)
		long[k] = strings.Repeat("x", n)
		if k == "file_url" {
			long[k] = "https://x.test/" + strings.Repeat("x", n)
		}
		if code := h.do(author, "POST", "/api/author/submissions", long, nil); code != 422 {
			t.Errorf("%d-character %s = %d", n, k, code)
		}
	}

	var upd store.Submission
	if code := h.do(admin, "PUT", fmt.Sprintf("/api/admin/submissions/%d", sub.ID), map[string]any{"status": "stocked", "sku": "Sti/Spr/1", "admin_note": "done"}, &upd); code != 200 || upd.Status != "stocked" {
		t.Fatalf("update = %d %+v", code, upd)
	}
	var mine struct{ Submissions []store.Submission }
	h.do(author, "GET", "/api/author/submissions", nil, &mine)
	if len(mine.Submissions) != 1 || mine.Submissions[0].SKU != "Sti/Spr/1" {
		t.Fatalf("mine = %+v", mine)
	}
}

func newJar() http.CookieJar {
	j, _ := cookiejar.New(nil)
	return j
}

func TestImportedInternalNotesAreAdminOnly(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	_, err := h.store.ImportRequest(ctx, importer.Request{
		Source: "fillout", ExternalID: "sub-1", Email: userEmail, Name: "Hacker", Address: usAddr,
		Lines: []importer.SKULine{{SKU: "Sti/A", Quantity: 1}}, Status: swag.StatusDispatched,
		InternalNote: "Payment URL: https://dashboard.stripe.com/secret", CreatedAt: time.Now(),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var mine struct{ Requests []map[string]any }
	h.do(h.client(userEmail), "GET", "/api/requests", nil, &mine)
	if len(mine.Requests) != 1 {
		t.Fatalf("imported request should show for its owner once they sign in: %+v", mine)
	}
	if _, ok := mine.Requests[0]["internal_note"]; ok {
		t.Errorf("requester can see internal_note: %v", mine.Requests[0])
	}
	if mine.Requests[0]["source"] != "fillout" {
		t.Errorf("source = %v", mine.Requests[0]["source"])
	}
	var all struct{ Requests []map[string]any }
	h.do(h.client(adminEmail), "GET", "/api/admin/requests", nil, &all)
	if all.Requests[0]["internal_note"] != "Payment URL: https://dashboard.stripe.com/secret" {
		t.Errorf("admin should see internal_note: %v", all.Requests[0])
	}
}

func TestLoginUsesTheOriginTheUserIsOn(t *testing.T) {
	h := newHarness(t)
	c := &http.Client{Jar: newJar(), CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	get := func(path string) *http.Response {
		req, _ := http.NewRequest("GET", h.srv.URL+path, nil)
		req.Host = "porygon:5173" // as forwarded by the Vite dev proxy over Tailscale
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp
	}
	loc, _ := url.Parse(get("/auth/login").Header.Get("Location"))
	if got := loc.Query().Get("redirect_uri"); got != "http://porygon:5173/auth/callback" {
		t.Fatalf("redirect_uri = %q", got)
	}
	resp := get("/auth/callback?code=code-porygon&state=" + url.QueryEscape(loc.Query().Get("state")))
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback = %d", resp.StatusCode)
	}
	// The browser stays on porygon:5173, so its session cookie belongs to that host.
	req, _ := http.NewRequest("GET", h.srv.URL+"/api/me", nil)
	req.Host = "porygon:5173"
	meResp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var me meResponse
	json.NewDecoder(meResp.Body).Decode(&me)
	meResp.Body.Close()
	if me.User == nil || me.User.Address == nil || me.User.Address.Line1 != "15 Falls Rd" {
		t.Fatalf("me should carry the HCA address for prefilling: %+v", me.User)
	}

	// Unknown hosts fall back to BASE_URL.
	req, _ = http.NewRequest("GET", h.srv.URL+"/auth/login", nil)
	req.Host = "evil.example"
	r2, _ := c.Do(req)
	r2.Body.Close()
	loc2, _ := url.Parse(r2.Header.Get("Location"))
	if got := loc2.Query().Get("redirect_uri"); got != "http://app.test/auth/callback" {
		t.Fatalf("fallback redirect_uri = %q", got)
	}
}

func TestMutationsAcceptConfiguredOrigins(t *testing.T) {
	h := newHarness(t)
	c := h.client(userEmail)
	for origin, want := range map[string]int{"http://porygon:5173": 422, "http://app.test": 422, "http://evil.example": 403} {
		req, _ := http.NewRequest("POST", h.srv.URL+"/api/requests", strings.NewReader(`{"lines":[]}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", origin)
		resp, _ := c.Do(req)
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("Origin %s -> %d, want %d", origin, resp.StatusCode, want)
		}
	}
}

func TestImportedRequestsCannotBeShippedAgain(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.store.ImportRequest(ctx, importer.Request{
		Source: "fillout", ExternalID: "sub-1", Email: userEmail, Address: usAddr,
		Lines: []importer.SKULine{{SKU: "Sti/A", Quantity: 1}}, Status: swag.StatusPending,
		AirtableRecordID: "recAAA", CreatedAt: time.Now(),
	}, nil)
	all, _ := h.store.ListRequests(ctx, "")
	if code := h.do(h.client(adminEmail), "POST", fmt.Sprintf("/api/admin/requests/%d/dispatch", all[0].ID), map[string]any{}, nil); code != 409 {
		t.Fatalf("dispatching an imported request = %d, want 409", code)
	}
	if len(h.wh.orders) != 0 {
		t.Fatal("no warehouse order may be created for imported requests")
	}
}

func TestLoginHonorsForwardedHostFromDevProxy(t *testing.T) {
	h := newHarness(t)
	c := h.client("")
	for fwd, want := range map[string]string{
		"porygon:5173":         "http://porygon:5173/auth/callback",
		"evil.example":         "http://app.test/auth/callback", // only allowlisted origins
		"porygon:5173, x.test": "http://porygon:5173/auth/callback",
	} {
		req, _ := http.NewRequest("GET", h.srv.URL+"/auth/login", nil)
		req.Header.Set("X-Forwarded-Host", fwd)
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		loc, _ := url.Parse(resp.Header.Get("Location"))
		if got := loc.Query().Get("redirect_uri"); got != want {
			t.Errorf("X-Forwarded-Host %q -> %q, want %q", fwd, got, want)
		}
	}
}

func TestAllowedEmailsLimitSignIn(t *testing.T) {
	h := newHarness(t)
	h.cfg.AllowedEmails = []string{adminEmail}

	// A signed-in session for someone outside the list counts as signed out.
	var me meResponse
	h.do(h.client(userEmail), "GET", "/api/me", nil, &me)
	if me.User != nil {
		t.Fatalf("session outside ALLOWED_EMAILS still signed in: %+v", me)
	}
	me = meResponse{}
	h.do(h.client(adminEmail), "GET", "/api/me", nil, &me)
	if me.User == nil || !me.Roles.Admin {
		t.Fatalf("allowed admin = %+v", me)
	}

	// The OAuth callback refuses them without starting a session.
	jar := newJar()
	c := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Get(h.srv.URL + "/auth/login")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	loc, _ := url.Parse(resp.Header.Get("Location"))
	resp, _ = c.Get(h.srv.URL + "/auth/callback?code=code-user&state=" + url.QueryEscape(loc.Query().Get("state")))
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("callback for a non-allowed email -> %d", resp.StatusCode)
	}
	if u, _ := url.Parse(h.srv.URL); len(jar.Cookies(u)) != 0 {
		for _, ck := range jar.Cookies(u) {
			if ck.Name == sessionCookie {
				t.Fatal("non-allowed email got a session cookie")
			}
		}
	}
}

func TestMagazineShipsWithItsCard(t *testing.T) {
	h := newHarness(t)
	admin := h.client(adminEmail)
	card := h.mkItem("Pri/M25/4x6/1st", false)
	card.Images = []string{"https://cdn.example/card.jpg"}
	h.do(admin, "PUT", fmt.Sprintf("/api/admin/items/%d", card.ID), card, nil)
	mag := h.mkItem("Pri/Bok/M25/1st", true)
	mag.ShipsWith = []string{"Pri/M25/4x6/1st"}
	if code := h.do(admin, "PUT", fmt.Sprintf("/api/admin/items/%d", mag.ID), mag, &mag); code != 200 || !slices.Equal(mag.ShipsWith, []string{"Pri/M25/4x6/1st"}) {
		t.Fatalf("save ships_with = %d %+v", code, mag)
	}

	// The catalog carries what each listed item comes with, even when that's hidden.
	var cat struct {
		Items    []swag.Item
		Includes []swag.Item
	}
	h.do(h.client(""), "GET", "/api/catalog", nil, &cat)
	if len(cat.Items) != 1 || len(cat.Includes) != 1 || cat.Includes[0].SKU != "Pri/M25/4x6/1st" || cat.Includes[0].Images[0] != "https://cdn.example/card.jpg" {
		t.Fatalf("catalog = %+v", cat)
	}

	var r store.Request
	h.do(h.client(userEmail), "POST", "/api/requests", map[string]any{"purpose": "personal", "address": usAddr, "lines": []swag.Line{{ItemID: mag.ID, Quantity: 2}}}, &r)
	if code := h.do(admin, "POST", fmt.Sprintf("/api/admin/requests/%d/dispatch", r.ID), map[string]any{}, nil); code != 200 {
		t.Fatalf("dispatch = %d", code)
	}
	want := []theseus.Content{{SKU: "Pri/Bok/M25/1st", Quantity: 2}, {SKU: "Pri/M25/4x6/1st", Quantity: 2}}
	if got := h.wh.orders[0].Contents; !slices.Equal(got, want) {
		t.Fatalf("contents = %+v, want %+v", got, want)
	}
}

func TestBookletDetailsAreValidated(t *testing.T) {
	h := newHarness(t)
	admin := h.client(adminEmail)
	it := h.mkItem("Pri/Bok/Mini25/1st", true)
	path := fmt.Sprintf("/api/admin/items/%d", it.ID)
	for name, bad := range map[string]swag.Item{
		"unknown binding":   {Binding: "spiral"},
		"page url":          {Pages: []string{"javascript:alert(1)"}},
		"blank ships_with":  {ShipsWith: []string{" "}},
		"ships with itself": {ShipsWith: []string{"Pri/Bok/Mini25/1st"}},
	} {
		bad.SKU, bad.Name, bad.MaxPerRequest = it.SKU, it.Name, it.MaxPerRequest
		if code := h.do(admin, "PUT", path, bad, nil); code != 422 {
			t.Errorf("%s = %d, want 422", name, code)
		}
	}
	it.Binding, it.Pages = "staple", []string{"https://cdn.example/p1.jpg", "https://cdn.example/p2.jpg"}
	var upd swag.Item
	if code := h.do(admin, "PUT", path, it, &upd); code != 200 || upd.Binding != "staple" || !slices.Equal(upd.Pages, it.Pages) || upd.ShipsWith == nil {
		t.Fatalf("good booklet = %d %+v", code, upd)
	}
}

func TestItemClipIsAnHTTPURL(t *testing.T) {
	h := newHarness(t)
	admin := h.client(adminEmail)
	it := h.mkItem("Pri/Bok/Mini25/1st", true)
	path := fmt.Sprintf("/api/admin/items/%d", it.ID)
	it.Clip = "javascript:alert(1)"
	if code := h.do(admin, "PUT", path, it, nil); code != 422 {
		t.Errorf("bad clip = %d, want 422", code)
	}
	it.Clip = "https://cdn.example/mini.mp4"
	var upd swag.Item
	if code := h.do(admin, "PUT", path, it, &upd); code != 200 || upd.Clip != it.Clip {
		t.Fatalf("good clip = %d %+v", code, upd)
	}
	var cat struct{ Items []swag.Item }
	h.do(h.client(userEmail), "GET", "/api/catalog", nil, &cat)
	if len(cat.Items) != 1 || cat.Items[0].Clip != it.Clip {
		t.Fatalf("catalog = %+v", cat.Items)
	}
}

func TestItemMinimum(t *testing.T) {
	h := newHarness(t)
	admin := h.client(adminEmail)
	it := h.mkItem("Pri/HPd/11x/1st", true)
	path := fmt.Sprintf("/api/admin/items/%d", it.ID)
	it.MinPerRequest, it.MaxPerRequest = 3, 2
	if code := h.do(admin, "PUT", path, it, nil); code != 422 {
		t.Errorf("min above max = %d, want 422", code)
	}
	it.MinPerRequest = 0
	var upd swag.Item
	if code := h.do(admin, "PUT", path, it, &upd); code != 200 || upd.MinPerRequest != 1 {
		t.Fatalf("no minimum = %d, min %d, want 1", code, upd.MinPerRequest)
	}
	it.MinPerRequest, it.MaxPerRequest = 2, 20
	if code := h.do(admin, "PUT", path, it, &upd); code != 200 || upd.MinPerRequest != 2 || upd.MaxPerRequest != 20 {
		t.Fatalf("2 to 20 = %d %+v", code, upd)
	}
	var cat struct{ Items []swag.Item }
	h.do(h.client(userEmail), "GET", "/api/catalog", nil, &cat)
	if len(cat.Items) != 1 || cat.Items[0].MinPerRequest != 2 {
		t.Fatalf("catalog = %+v", cat.Items)
	}
	var errBody map[string]string
	one := map[string]any{"purpose": "personal", "address": usAddr, "lines": []swag.Line{{ItemID: it.ID, Quantity: 1}}}
	if code := h.do(h.client(userEmail), "POST", "/api/requests", one, &errBody); code != 422 || !strings.Contains(errBody["error"], "at least 2") {
		t.Fatalf("one poster = %d %v", code, errBody)
	}
}

func TestItemQuickPicks(t *testing.T) {
	h := newHarness(t)
	admin := h.client(adminEmail)
	it := h.mkItem("Pri/Bok/M25/1st", true)
	path := fmt.Sprintf("/api/admin/items/%d", it.ID)
	it.MaxPerRequest = 25
	for _, bad := range [][]int{{0, 3}, {1, 30}, {5, 3}} {
		it.QuickPicks = bad
		if code := h.do(admin, "PUT", path, it, nil); code != 422 {
			t.Errorf("quick picks %v = %d, want 422", bad, code)
		}
	}
	it.QuickPicks = []int{1, 3, 5, 10}
	var upd swag.Item
	if code := h.do(admin, "PUT", path, it, &upd); code != 200 || fmt.Sprint(upd.QuickPicks) != "[1 3 5 10]" {
		t.Fatalf("good quick picks = %d %+v", code, upd.QuickPicks)
	}
	it.QuickPicks = nil
	if code := h.do(admin, "PUT", path, it, &upd); code != 200 || upd.QuickPicks == nil || len(upd.QuickPicks) != 0 {
		t.Fatalf("cleared = %d %#v", code, upd.QuickPicks)
	}
}

func TestAdminUploadsItemPages(t *testing.T) {
	h := newHarness(t)
	it := h.mkItem("Pri/Bok/Mini25/1st", true)
	var buf bytes.Buffer
	png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 30, 30)))
	path := fmt.Sprintf("/api/admin/items/%d/pages", it.ID)
	var upd swag.Item
	for _, name := range []string{"cover.png", "page 2.png"} {
		body := map[string]string{"filename": name, "data": base64.StdEncoding.EncodeToString(buf.Bytes())}
		if code := h.do(h.client(adminEmail), "POST", path, body, &upd); code != 200 {
			t.Fatalf("page upload = %d", code)
		}
	}
	if want := []string{"https://cdn.example/1/cover.jpg", "https://cdn.example/2/page-2.jpg"}; !slices.Equal(upd.Pages, want) || len(upd.Images) != 0 {
		t.Fatalf("pages = %v images = %v", upd.Pages, upd.Images)
	}
	if code := h.do(h.client(userEmail), "POST", path, map[string]string{"filename": "x.png"}, nil); code != 403 {
		t.Fatalf("non-admin page upload = %d", code)
	}
}

// My requests shows each request in 3D, so it gets every item in the requester's
// lines (even ones hidden since) and what they ship with, and nothing else.
func TestMyRequestsIncludeTheirItems(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	mag := h.mkItem("Pri/Bok/Mini25/1st", true)
	card := h.mkItem("Pri/M25/4x6/1st", false)
	other := h.mkItem("Sti/Other", true)
	mag.ShipsWith = []string{card.SKU}
	if _, err := h.store.UpdateItem(ctx, mag); err != nil {
		t.Fatal(err)
	}
	user := h.client(userEmail)
	if code := h.do(user, "POST", "/api/requests", map[string]any{"purpose": "personal", "address": usAddr, "lines": []swag.Line{{ItemID: mag.ID, Quantity: 2}}}, nil); code != 201 {
		t.Fatalf("create = %d", code)
	}
	mag.Visible = false
	if _, err := h.store.UpdateItem(ctx, mag); err != nil {
		t.Fatal(err)
	}
	var mine struct {
		Requests []store.Request
		Items    []swag.Item
	}
	h.do(user, "GET", "/api/requests", nil, &mine)
	ids := map[int64]bool{}
	for _, it := range mine.Items {
		ids[it.ID] = true
	}
	if len(mine.Items) != 2 || !ids[mag.ID] || !ids[card.ID] || ids[other.ID] {
		t.Fatalf("items = %+v", mine.Items)
	}

	var none struct{ Items []swag.Item }
	h.do(h.client("a@x.com"), "GET", "/api/requests", nil, &none)
	if none.Items == nil || len(none.Items) != 0 {
		t.Fatalf("no requests: items = %+v", none.Items)
	}
}
