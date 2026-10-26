// Package httpapi wires HTTP routes: Hack Club Auth login, the JSON API for
// the three user flows (hack clubbers, YSWS authors, admin), and the embedded
// PWA frontend.
package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hackclub/merch/internal/auth"
	"github.com/hackclub/merch/internal/authors"
	"github.com/hackclub/merch/internal/config"
	"github.com/hackclub/merch/internal/imgprep"
	"github.com/hackclub/merch/internal/store"
	"github.com/hackclub/merch/internal/stripe"
	"github.com/hackclub/merch/internal/swag"
	"github.com/hackclub/merch/internal/theseus"
)

const (
	sessionCookie = "zmr_session"
	stateCookie   = "zmr_oauth_state"
	sessionTTL    = 30 * 24 * time.Hour
)

// Warehouse is the subset of the mail.hackclub.com client the API uses.
type Warehouse interface {
	ListSKUs(ctx context.Context) ([]theseus.SKU, error)
	CreateOrder(ctx context.Context, in theseus.OrderInput) (*theseus.Order, error)
	GetOrder(ctx context.Context, id string) (*theseus.Order, error)
}

// ImageStore hosts item images (cdn.hackclub.com in production).
type ImageStore interface {
	Upload(ctx context.Context, filename, contentType string, data []byte) (url string, err error)
	// UploadStream sends size bytes (-1: unknown) read from r as they arrive.
	UploadStream(ctx context.Context, filename, contentType string, r io.Reader, size int64) (url string, err error)
}

type Deps struct {
	Config    *config.Config
	Store     *store.Store
	Provider  auth.Provider
	Authors   authors.Directory
	Warehouse Warehouse
	Images    ImageStore    // nil disables image uploads
	Places    AddressFinder // nil disables address autocomplete
	Payments  Payments      // card payments for international shipping (Stripe)
	Mailer    Mailer        // nil sends no emails
	Static    fs.FS         // built PWA (web/build); nil disables static serving
	Now       func() time.Time
	Log       *slog.Logger
	Warm      bool // load caches (shipping history) in the background at start
	RateLimit bool // per-client limits on /api and /auth (guard.go)
}

type server struct {
	Deps

	skuMu         sync.Mutex
	skus          []theseus.SKU
	skusAt        time.Time
	skuRefreshing bool       // a background refresh of skus is running
	skuFetch      sync.Mutex // one warehouse fetch at a time

	history historyCache

	release string // the web build being served, stamped on usage events
}

// skuCacheTTL bounds how often product pages hit mail.hackclub.com. Past it, the list is still
// served while a fresh one loads in the background, up to skuStaleMax.
const (
	skuCacheTTL = time.Minute
	skuStaleMax = 30 * time.Minute
)

func New(d Deps) http.Handler {
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Log == nil {
		d.Log = slog.Default()
	}
	s := &server{Deps: d, release: buildRelease(d.Static)}
	if d.Warm {
		go s.warm(context.Background())
	}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /auth/login", s.login)
	mux.HandleFunc("GET /auth/callback", s.callback)
	mux.HandleFunc("POST /auth/logout", s.logout)

	mux.HandleFunc("GET /api/me", s.me)
	mux.HandleFunc("GET /api/catalog", s.catalog)
	mux.HandleFunc("GET /api/items/{id}/stock", s.itemStock)
	mux.HandleFunc("GET /api/shipping", s.shippingSummary)
	mux.HandleFunc("POST /api/shipping/estimate", s.shippingEstimate)
	mux.HandleFunc("POST /api/events", s.clientEvents)

	// hack clubbers
	mux.Handle("GET /api/requests", s.requireUser(s.myRequests))
	mux.Handle("POST /api/requests", s.requireUser(s.createRequest))
	mux.Handle("POST /api/requests/{id}/cancel", s.requireUser(s.cancelRequest))
	mux.Handle("POST /api/requests/{id}/pay", s.requireUser(s.payRequest))
	mux.Handle("GET /api/requests/{id}/customs-invoice", s.requireUser(s.customsInvoice))
	mux.Handle("GET /api/address/suggest", s.requireUser(s.addressSuggest))
	mux.Handle("GET /api/address/place", s.requireUser(s.addressPlace))

	// staff getting postcards and posters printed (print.go)
	mux.HandleFunc("GET /api/print/products", s.printProducts)
	mux.HandleFunc("GET /api/print/estimate", s.printEstimate)
	mux.Handle("POST /api/print/files", s.requireUser(s.uploadPrintFile))
	mux.Handle("GET /api/print-requests", s.requireUser(s.myPrintRequests))
	mux.Handle("POST /api/print-requests", s.requireUser(s.createPrintRequest))

	// YSWS authors
	mux.Handle("GET /api/author/submissions", s.requireAuthor(s.mySubmissions))
	mux.Handle("POST /api/author/submissions", s.requireAuthor(s.createSubmission))

	// admin
	mux.Handle("GET /api/admin/requests", s.requireAdmin(s.adminRequests))
	mux.Handle("GET /api/admin/dashboard", s.requireAdmin(s.adminDashboard))
	mux.Handle("POST /api/admin/requests/refresh", s.requireAdmin(s.adminRefreshAll))
	mux.Handle("POST /api/admin/requests/{id}/dispatch", s.requireAdmin(s.adminDispatch))
	mux.Handle("POST /api/admin/requests/{id}/reject", s.requireAdmin(s.adminReject))
	mux.Handle("POST /api/admin/requests/{id}/lines", s.requireAdmin(s.adminReduceLines))
	mux.Handle("POST /api/admin/requests/{id}/refresh", s.requireAdmin(s.adminRefresh))
	mux.Handle("PUT /api/admin/requests/{id}/internal-note", s.requireAdmin(s.adminInternalNote))
	mux.Handle("GET /api/admin/items", s.requireAdmin(s.adminItems))
	mux.Handle("POST /api/admin/items", s.requireAdmin(s.adminCreateItem))
	mux.Handle("PUT /api/admin/items/{id}", s.requireAdmin(s.adminUpdateItem))
	mux.Handle("POST /api/admin/items/{id}/images", s.requireAdmin(s.adminUploadItemImage))
	mux.Handle("POST /api/admin/items/{id}/photos", s.requireAdmin(s.adminUploadItemPhoto))
	mux.Handle("POST /api/admin/items/{id}/print-files", s.requireAdmin(s.adminUploadPrintFile))
	mux.Handle("POST /api/admin/items/{id}/pages", s.requireAdmin(s.adminUploadItemPage))
	mux.Handle("GET /api/admin/warehouse/skus", s.requireAdmin(s.adminSKUs))
	mux.Handle("GET /api/admin/settings", s.requireAdmin(s.adminSettings))
	mux.Handle("PUT /api/admin/settings", s.requireAdmin(s.adminUpdateSettings))
	mux.Handle("GET /api/admin/submissions", s.requireAdmin(s.adminSubmissions))
	mux.Handle("PUT /api/admin/submissions/{id}", s.requireAdmin(s.adminUpdateSubmission))
	mux.Handle("GET /api/admin/print-requests", s.requireAdmin(s.adminPrintRequests))
	mux.Handle("PUT /api/admin/print-requests/{id}", s.requireAdmin(s.adminUpdatePrintRequest))
	mux.Handle("GET /api/admin/api-keys", s.requireAdmin(s.adminAPIKeys))
	mux.Handle("POST /api/admin/api-keys", s.requireAdmin(s.adminCreateAPIKey))
	mux.Handle("POST /api/admin/api-keys/{id}/revoke", s.requireAdmin(s.adminRevokeAPIKey))
	mux.Handle("GET /api/admin/emails", s.requireAdmin(s.adminEmails))

	// agents reviewing requests, with an admin's API key (agent.go)
	mux.HandleFunc("GET /api/agent", s.agentDocs)
	mux.Handle("GET /api/agent/requests", s.requireAgent(s.adminRequests))
	mux.Handle("GET /api/agent/requests/{id}", s.requireAgent(s.agentRequest))
	mux.Handle("POST /api/agent/requests/{id}/notes", s.requireAgent(s.agentNote))
	mux.Handle("POST /api/agent/requests/{id}/dispatch", s.requireAgent(s.adminDispatch))
	mux.Handle("POST /api/agent/requests/{id}/reject", s.requireAgent(s.adminReject))
	mux.Handle("POST /api/agent/requests/{id}/lines", s.requireAgent(s.adminReduceLines))
	mux.Handle("POST /api/agent/requests/refresh", s.requireAgent(s.adminRefreshAll))
	mux.Handle("POST /api/agent/requests/{id}/refresh", s.requireAgent(s.adminRefresh))
	mux.Handle("GET /api/agent/items", s.requireAgent(s.adminItems))
	mux.Handle("GET /api/agent/emails", s.requireAgent(s.adminEmails))
	mux.Handle("GET /api/agent/print-requests", s.requireAgent(s.adminPrintRequests))

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { writeErr(w, http.StatusNotFound, "not found") })
	mux.HandleFunc("/", s.static)

	return s.headers(s.csrf(s.visitor(s.logAPI(s.limit(gzipJSON(mux))))))
}

// ---- middleware ----

// csrf rejects state-changing API calls that aren't same-origin JSON. Browsers
// can't send cross-site application/json without a CORS preflight we never
// grant, and SameSite=Lax session cookies cover the rest. Print art uploads
// as the image's own bytes (image/png, image/jpeg), which need the same
// preflight, so /api/print/files admits those too.
func (s *server) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead && strings.HasPrefix(r.URL.Path, "/api/") {
			if o := r.Header.Get("Origin"); o != "" && !s.Config.IsAllowedOrigin(o) {
				writeErr(w, http.StatusForbidden, "cross-origin request refused")
				return
			}
			mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if mt != "application/json" && !(r.URL.Path == "/api/print/files" && (mt == "image/png" || mt == "image/jpeg")) {
				writeErr(w, http.StatusUnsupportedMediaType, "send application/json")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

type ctxKey struct{}

type viewer struct {
	user  *store.User
	roles roles
	key   *store.APIKey // set when an agent calls with an API key
}

type roles struct {
	Author bool `json:"author"`
	Admin  bool `json:"admin"`
}

func (s *server) currentUser(r *http.Request) *store.User {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return nil
	}
	u, err := s.Store.UserBySession(r.Context(), c.Value)
	if err != nil || !s.Config.CanSignIn(u.Email) {
		return nil
	}
	return u
}

func (s *server) rolesFor(ctx context.Context, u *store.User) roles {
	if u == nil {
		return roles{}
	}
	if s.Config.IsAdminEmail(u.Email) {
		return roles{Admin: true, Author: true}
	}
	ok, err := s.Authors.IsAuthor(ctx, u.Email)
	if err != nil {
		s.Log.Warn("ysws author lookup failed", "err", err)
	}
	return roles{Author: ok}
}

type handler func(w http.ResponseWriter, r *http.Request, v viewer)

func (s *server) requireRole(need func(roles) bool, h handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := s.currentUser(r)
		if u == nil {
			writeErr(w, http.StatusUnauthorized, "sign in first")
			return
		}
		v := viewer{user: u, roles: s.rolesFor(r.Context(), u)}
		if !need(v.roles) {
			writeErr(w, http.StatusForbidden, "you don't have access to this")
			return
		}
		h(w, r, v)
	})
}

func (s *server) requireUser(h handler) http.Handler {
	return s.requireRole(func(roles) bool { return true }, h)
}
func (s *server) requireAuthor(h handler) http.Handler {
	return s.requireRole(func(r roles) bool { return r.Author }, h)
}
func (s *server) requireAdmin(h handler) http.Handler {
	return s.requireRole(func(r roles) bool { return r.Admin }, h)
}

// ---- auth ----

func (s *server) healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.Store.Ping(ctx); err != nil {
		writeErr(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// origin is the configured origin the user is on (BASE_URL or one of
// EXTRA_BASE_URLS), matched by X-Forwarded-Host (set by the Vite dev proxy and
// ingresses) or else Host. OAuth callbacks and cookies stay on it. Trusting the
// header is safe: it can only select among allowlisted origins.
func (s *server) origin(r *http.Request) string {
	if o, ok := s.Config.LookupOrigin(strings.Split(r.Header.Get("X-Forwarded-Host"), ",")[0]); ok {
		return o
	}
	return s.Config.OriginForHost(r.Host)
}

func (s *server) secure(r *http.Request) bool { return strings.HasPrefix(s.origin(r), "https://") }

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	b := make([]byte, 24)
	rand.Read(b)
	state := base64.RawURLEncoding.EncodeToString(b)
	props := map[string]any{"referrer": clip(r.Referer(), 300)}
	switch f := r.URL.Query().Get("for"); f {
	case "request", "print": // Submit on the request form verifies their email; staff sign in to upload art for printing
		props["for"] = f
	}
	s.track(r, nil, "login_start", props)
	http.SetCookie(w, &http.Cookie{
		Name: stateCookie, Value: state, Path: "/auth", MaxAge: 600,
		HttpOnly: true, Secure: s.secure(r), SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, s.Provider.AuthCodeURL(state, s.origin(r)+"/auth/callback"), http.StatusFound)
}

func (s *server) callback(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(stateCookie)
	state := r.URL.Query().Get("state")
	if err != nil || state == "" || subtle.ConstantTimeCompare([]byte(c.Value), []byte(state)) != 1 {
		http.Error(w, "login expired or invalid state — try again", http.StatusBadRequest)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: stateCookie, Path: "/auth", MaxAge: -1})
	if e := r.URL.Query().Get("error"); e != "" {
		http.Error(w, "sign-in was cancelled: "+e, http.StatusBadRequest)
		return
	}
	id, err := s.Provider.Exchange(r.Context(), r.URL.Query().Get("code"), s.origin(r)+"/auth/callback")
	if err != nil {
		s.Log.Warn("hca exchange failed", "err", err)
		if s.currentUser(r) != nil { // a second load of a callback that already signed them in
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		if sw, ok := w.(*statusWriter); ok {
			sw.err = err
		}
		http.Error(w, "couldn't sign you in with Hack Club Auth", http.StatusBadGateway)
		return
	}
	if !s.Config.CanSignIn(id.Email) {
		s.Log.Info("sign-in refused: email not in ALLOWED_EMAILS")
		s.track(r, nil, "sign_in_refused", nil)
		http.Error(w, "This site isn't open yet. Only invited accounts can sign in.", http.StatusForbidden)
		return
	}
	u, err := s.Store.UpsertUser(r.Context(), *id)
	if err != nil {
		s.serverErr(w, err)
		return
	}
	tok, err := s.Store.CreateSession(r.Context(), u.ID, sessionTTL)
	if err != nil {
		s.serverErr(w, err)
		return
	}
	s.track(r, u, "sign_in", map[string]any{"verification_status": u.VerificationStatus, "has_address": u.Address != nil})
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: tok, Path: "/", MaxAge: int(sessionTTL.Seconds()),
		HttpOnly: true, Secure: s.secure(r), SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.track(r, s.currentUser(r), "sign_out", nil)
		s.Store.DeleteSession(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
}

type meResponse struct {
	User  *store.User `json:"user"`
	Roles roles       `json:"roles"`
}

func (s *server) me(w http.ResponseWriter, r *http.Request) {
	u := s.currentUser(r)
	writeJSON(w, http.StatusOK, meResponse{User: u, Roles: s.rolesFor(r.Context(), u)})
}

// ---- hack clubber flow ----

func (s *server) catalog(w http.ResponseWriter, r *http.Request) {
	items, err := s.Store.ListItems(r.Context(), true)
	if err != nil {
		s.serverErr(w, err)
		return
	}
	st, err := s.Store.GetSettings(r.Context())
	if err != nil {
		s.serverErr(w, err)
		return
	}
	// What listed items ship with (the magazine's card), so their pages can
	// show it; these are often hidden items.
	var with []string
	for _, it := range items {
		with = append(with, it.ShipsWith...)
	}
	includes := []swag.Item{}
	if len(with) > 0 {
		if includes, err = s.Store.ItemsBySKU(r.Context(), with); err != nil {
			s.serverErr(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":    items,
		"includes": includes,
		"stock":    s.catalogStock(r.Context(), items),
		"settings": map[string]any{
			"requests_open":                    st.RequestsOpen,
			"international_shipping_fee_cents": st.InternationalShippingFeeCents,
			"stripe_key":                       s.Config.StripePublishableKey,
		},
	})
}

// itemStock reports a listed item's live warehouse stock and unit cost.
func (s *server) itemStock(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	it, err := s.Store.GetItem(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) || (err == nil && !it.Visible) {
		writeErr(w, http.StatusNotFound, "item not found")
		return
	} else if err != nil {
		s.serverErr(w, err)
		return
	}
	skus, err := s.cachedSKUs(r.Context())
	if err != nil {
		s.Log.Error("warehouse sku lookup failed", "err", err)
		writeErr(w, http.StatusBadGateway, "the warehouse didn't answer; try again soon")
		return
	}
	type variantStock struct {
		SKU      string  `json:"sku"`
		Label    string  `json:"label"`
		InStock  int     `json:"in_stock"`
		UnitCost *string `json:"unit_cost"`
	}
	out := struct {
		InStock     int            `json:"in_stock"`
		UnitCost    *string        `json:"unit_cost"`
		UnitCostMax *string        `json:"unit_cost_max"` // set when sizes cost different amounts
		Variants    []variantStock `json:"variants"`      // per size, in the item's order
	}{Variants: []variantStock{}}
	// A sized item's stock is its sizes' SKUs; otherwise its own SKU.
	want := []string{it.SKU}
	if len(it.Variants) > 0 {
		want = want[:0]
		for _, v := range it.Variants {
			want = append(want, v.SKU)
			vs := theseus.StockFor(skus, v.SKU)
			var cost *string
			if vs.MinCost != "" {
				cost = &vs.MinCost
			}
			out.Variants = append(out.Variants, variantStock{v.SKU, v.Label, vs.InStock, cost})
		}
	}
	st := theseus.StockFor(skus, want...)
	out.InStock = st.InStock
	if st.MinCost != "" {
		out.UnitCost = &st.MinCost
	}
	if st.MaxCost != st.MinCost {
		out.UnitCostMax = &st.MaxCost
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) cachedSKUs(ctx context.Context) ([]theseus.SKU, error) {
	s.skuMu.Lock()
	if s.skus != nil {
		age := s.Now().Sub(s.skusAt)
		if age < skuCacheTTL {
			defer s.skuMu.Unlock()
			return s.skus, nil
		}
		// A minute old: answer with it now and refresh in the background, so no visitor waits on
		// the warehouse (catalog calls that did took up to 15 s).
		if age < skuStaleMax {
			if !s.skuRefreshing {
				s.skuRefreshing = true
				go s.refreshSKUs()
			}
			defer s.skuMu.Unlock()
			return s.skus, nil
		}
	}
	s.skuMu.Unlock()
	return s.fetchSKUs(ctx)
}

// fetchSKUs loads the warehouse's SKU list into the cache, one fetch at a time.
func (s *server) fetchSKUs(ctx context.Context) ([]theseus.SKU, error) {
	s.skuFetch.Lock()
	defer s.skuFetch.Unlock()
	s.skuMu.Lock()
	if s.skus != nil && s.Now().Sub(s.skusAt) < skuCacheTTL {
		defer s.skuMu.Unlock()
		return s.skus, nil
	}
	s.skuMu.Unlock()
	list, err := s.Warehouse.ListSKUs(ctx)
	if err != nil {
		return nil, err
	}
	s.skuMu.Lock()
	defer s.skuMu.Unlock()
	s.skus, s.skusAt = list, s.Now()
	return list, nil
}

func (s *server) refreshSKUs() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := s.fetchSKUs(ctx); err != nil {
		s.Log.Error("warehouse sku refresh failed", "err", err)
	}
	s.skuMu.Lock()
	s.skuRefreshing = false
	s.skuMu.Unlock()
}

type requestView struct {
	*store.Request
	StripeKey string         `json:"stripe_key,omitempty"` // pay by card on the page, with this publishable key
	Estimate  *swag.Estimate `json:"estimate,omitempty"`   // when it should arrive, while it waits to ship
	Costs     *swag.Costs    `json:"costs,omitempty"`      // admins: what it costs Hack Club to send
	// CostsActual names the parts of a shipped request's Costs that are the
	// warehouse's own charges ("items", "labor", "postage"); the rest are estimates.
	CostsActual []string `json:"costs_actual,omitempty"`
}

func (s *server) view(r *store.Request) requestView {
	v := requestView{Request: r}
	if r.Status == swag.StatusAwaitingPayment {
		v.StripeKey = s.Config.StripePublishableKey
	}
	return v
}

// userView is what a requester sees: no admin-only notes.
func (s *server) userView(r *store.Request) requestView {
	cp := *r
	cp.InternalNote = ""
	return s.view(&cp)
}

func (s *server) views(rs []*store.Request) []requestView {
	out := make([]requestView, len(rs))
	for i, r := range rs {
		out[i] = s.view(r)
	}
	return out
}

func (s *server) myRequests(w http.ResponseWriter, r *http.Request, v viewer) {
	rs, err := s.Store.ListRequestsByUser(r.Context(), v.user.ID)
	if err != nil {
		s.serverErr(w, err)
		return
	}
	s.syncPayments(r, rs)
	out := make([]requestView, len(rs))
	for i, r := range rs {
		out[i] = s.userView(r)
	}
	if err := s.withEstimates(r.Context(), out, false); err != nil {
		s.serverErr(w, err)
		return
	}
	items, err := s.requestItems(r.Context(), rs)
	if err != nil {
		s.serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": out, "items": items})
}

// requestItems is every item in the requests' lines, hidden ones too, and what
// they ship with, so My requests can show each request in 3D.
func (s *server) requestItems(ctx context.Context, rs []*store.Request) ([]swag.Item, error) {
	out := []swag.Item{}
	ids := map[int64]bool{}
	for _, r := range rs {
		for _, l := range r.Lines {
			ids[l.ItemID] = true
		}
	}
	if len(ids) == 0 {
		return out, nil
	}
	all, err := s.Store.ListItems(ctx, false)
	if err != nil {
		return nil, err
	}
	with := map[string]bool{}
	for _, it := range all {
		if ids[it.ID] {
			for _, sku := range it.ShipsWith {
				with[sku] = true
			}
		}
	}
	for _, it := range all {
		if ids[it.ID] || with[it.SKU] {
			out = append(out, it)
		}
	}
	return out, nil
}

func (s *server) createRequest(w http.ResponseWriter, r *http.Request, v viewer) {
	var body struct {
		Purpose swag.Purpose `json:"purpose"`
		Address swag.Address `json:"address"`
		Lines   []swag.Line  `json:"lines"`
		Note    string       `json:"note"`
		// Outside the US: they'll pay their country's customs fees.
		CustomsOK bool `json:"customs_ok"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	body.Address.Country = strings.ToUpper(strings.TrimSpace(body.Address.Country))
	if err := body.Address.Validate(); err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if len(body.Note) > 1000 {
		writeErr(w, http.StatusUnprocessableEntity, "note is too long")
		return
	}
	st, err := s.Store.GetSettings(r.Context())
	if err != nil {
		s.serverErr(w, err)
		return
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
	// International requests pay the estimated postage for their package.
	_, est, err := s.quote(r.Context(), st, body.Address.Country, body.Lines, cat)
	if err != nil && !isRuleErr(err) {
		s.serverErr(w, err)
		return
	}
	status, fee := swag.InitialStatus(st, body.Address.Country, est)
	avail := s.availableStock(r.Context(), items)
	req, err := s.Store.CreateRequest(r.Context(), store.NewRequest{
		UserID: v.user.ID, Address: body.Address, Purpose: body.Purpose, Lines: body.Lines, Status: status, ShippingFeeCents: fee, Note: body.Note,
	}, func(prior map[int64]int) error {
		return swag.Validate(swag.ValidateInput{
			Settings: st, Catalog: cat, Purpose: body.Purpose, Reason: body.Note, Lines: body.Lines, Country: body.Address.Country, CustomsOK: body.CustomsOK, PriorQuantities: prior, Available: avail,
		})
	})
	if err != nil {
		if isRuleErr(err) {
			s.track(r, v.user, "request_refused", map[string]any{
				"error": err.Error(), "purpose": body.Purpose, "country": body.Address.Country, "lines": body.Lines, "items": itemCount(body.Lines),
			})
			writeErr(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		s.serverErr(w, err)
		return
	}
	s.track(r, v.user, "request_created", map[string]any{
		"request_id": req.ID, "status": req.Status, "purpose": body.Purpose, "country": body.Address.Country,
		"lines": body.Lines, "items": itemCount(body.Lines), "fee_cents": fee, "note_chars": len(body.Note),
		"package": est.Package, "arrive_from": est.ArriveFrom, "arrive_by": est.ArriveBy,
	})
	if req.Status == swag.StatusPending {
		s.emailRequest(r, req, "received")
	}
	writeJSON(w, http.StatusCreated, s.userView(req))
}

func isRuleErr(err error) bool {
	for _, e := range []error{swag.ErrRequestsClosed, swag.ErrNoItems, swag.ErrBadQuantity, swag.ErrUnknownItem,
		swag.ErrDuplicateItem, swag.ErrBadVariant, swag.ErrItemLimit, swag.ErrBadAddress, swag.ErrBadPurpose, swag.ErrNoReason, swag.ErrNoCustoms,
		swag.ErrWontPack, swag.ErrTooHeavy, swag.ErrOutOfStock} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

func (s *server) cancelRequest(w http.ResponseWriter, r *http.Request, v viewer) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	req, err := s.Store.GetRequest(r.Context(), id)
	if err != nil || req.UserID != v.user.ID {
		writeErr(w, http.StatusNotFound, "request not found")
		return
	}
	if !swag.CanTransition(req.Status, swag.StatusCancelled) {
		s.respondRequest(w, nil, fmt.Errorf("%w (%s -> %s)", store.ErrBadTransition, req.Status, swag.StatusCancelled))
		return
	}
	// Record a payment that just went through, close an unpaid one, and refund a paid one.
	req, refunded, err := s.settlePayment(r, req, "requester")
	if err != nil {
		writeErr(w, http.StatusBadGateway, "couldn't refund your card, so the request is still open; try again in a minute")
		return
	}
	from := req.Status
	req, err = s.Store.CloseRequest(r.Context(), id, swag.StatusCancelled, "", refunded)
	if err == nil {
		req.InternalNote = ""
		s.track(r, v.user, "request_cancelled", map[string]any{"request_id": id, "from": from})
	}
	s.respondRequest(w, req, err)
}

// ---- YSWS author flow ----

func (s *server) mySubmissions(w http.ResponseWriter, r *http.Request, v viewer) {
	subs, err := s.Store.ListSubmissionsByUser(r.Context(), v.user.ID)
	if err != nil {
		s.serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"submissions": subs})
}

func (s *server) createSubmission(w http.ResponseWriter, r *http.Request, v viewer) {
	var body store.Submission
	if !readJSON(w, r, &body) {
		return
	}
	body.Program, body.Title = strings.TrimSpace(body.Program), strings.TrimSpace(body.Title)
	if body.Kind == "" {
		body.Kind = "sticker"
	}
	switch {
	case body.Program == "" || body.Title == "":
		writeErr(w, http.StatusUnprocessableEntity, "program and title are required")
		return
	case !isHTTPURL(body.FileURL):
		writeErr(w, http.StatusUnprocessableEntity, "file_url must be an http(s) link to the print-ready file")
		return
	case body.Quantity < 1 || body.Quantity > 100000:
		writeErr(w, http.StatusUnprocessableEntity, "quantity must be between 1 and 100000")
		return
	case len(body.Program) > 200 || len(body.Title) > 200 || len(body.Kind) > 40 || len(body.Description) > 5000 || len(body.FileURL) > 2000:
		writeErr(w, http.StatusUnprocessableEntity, "program, title, kind, description or file_url is too long")
		return
	}
	body.UserID = v.user.ID
	sub, err := s.Store.CreateSubmission(r.Context(), body)
	if err != nil {
		s.serverErr(w, err)
		return
	}
	s.track(r, v.user, "submission_created", map[string]any{"submission_id": sub.ID, "kind": sub.Kind, "program": sub.Program, "quantity": sub.Quantity})
	writeJSON(w, http.StatusCreated, sub)
}

func isHTTPURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}

// ---- admin ----

func (s *server) adminRequests(w http.ResponseWriter, r *http.Request, v viewer) {
	rs, err := s.Store.ListRequests(r.Context(), swag.Status(r.URL.Query().Get("status")))
	if err != nil {
		s.serverErr(w, err)
		return
	}
	s.syncPayments(r, rs)
	out := s.views(rs)
	if err := s.withEstimates(r.Context(), out, true); err != nil {
		s.serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": out})
}

func (s *server) adminReject(w http.ResponseWriter, r *http.Request, v viewer) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct {
		Note string `json:"note"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	req, err := s.Store.GetRequest(r.Context(), id)
	if err != nil {
		s.respondRequest(w, nil, err)
		return
	}
	if !swag.CanTransition(req.Status, swag.StatusRejected) {
		writeErr(w, http.StatusConflict, "request is "+string(req.Status)+"; it can't be rejected")
		return
	}
	req, refunded, err := s.settlePayment(r, req, "admin")
	if err != nil {
		var held *stripe.ApprovalError
		if errors.As(err, &held) {
			writeErr(w, http.StatusBadGateway, fmt.Sprintf("Stripe is holding the %s refund until someone approves it in the Stripe dashboard (%s). The request wasn't rejected: once the refund goes through, reject it again.", dollars(req.ShippingFeeCents), held.ID))
			return
		}
		writeErr(w, http.StatusBadGateway, "couldn't refund the card, so the request wasn't rejected: "+err.Error())
		return
	}
	from := req.Status
	req, err = s.Store.CloseRequest(r.Context(), id, swag.StatusRejected, body.Note, refunded)
	if err == nil {
		s.trackRequest(r, v, "request_rejected", req, map[string]any{"from": from, "has_note": body.Note != "", "refunded": refunded})
		s.emailRequest(r, req, "rejected")
	}
	s.respondRequest(w, req, err)
}

// adminDispatch creates the warehouse order in mail.hackclub.com. The
// idempotency key is derived from the request id, so a retry after a timeout
// replays the same Theseus order instead of shipping twice. The admin can
// change quantities first (0 removes a line), add a visible item the requester
// didn't ask for (up to its max_per_request), and add a note to the requester.
func (s *server) adminDispatch(w http.ResponseWriter, r *http.Request, v viewer) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct {
		Lines []struct {
			SKU      string `json:"sku"`
			Quantity int    `json:"quantity"`
		} `json:"lines"`
		Note string `json:"note"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if len(body.Note) > 1000 {
		writeErr(w, http.StatusUnprocessableEntity, "note is too long")
		return
	}
	req, err := s.Store.GetRequest(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "request not found")
		return
	} else if err != nil {
		s.serverErr(w, err)
		return
	}
	if req.Source != "app" {
		writeErr(w, http.StatusConflict, "imported requests ship through the Airtable warehouse base, not from here")
		return
	}
	if !swag.CanTransition(req.Status, swag.StatusDispatched) {
		writeErr(w, http.StatusConflict, "request is "+string(req.Status)+"; only pending requests can be dispatched")
		return
	}
	changes := store.ShipChanges{Quantities: map[string]int{}, Note: body.Note}
	for _, l := range body.Lines {
		if l.Quantity < 0 || l.Quantity > 10000 {
			writeErr(w, http.StatusUnprocessableEntity, "quantities must be between 0 and 10000")
			return
		}
		if slices.ContainsFunc(req.Lines, func(rl store.RequestLine) bool { return rl.SKU == l.SKU }) {
			changes.Quantities[l.SKU] = l.Quantity
			continue
		}
		if l.Quantity == 0 || slices.ContainsFunc(changes.Added, func(a swag.Line) bool { return a.SKU == l.SKU }) {
			continue
		}
		found, err := s.Store.ItemsBySKU(r.Context(), []string{l.SKU})
		if err != nil {
			s.serverErr(w, err)
			return
		}
		if len(found) != 1 || !found[0].Visible || len(found[0].Variants) > 0 {
			writeErr(w, http.StatusUnprocessableEntity, l.SKU+" isn't in this request, and only a listed item without sizes can be added")
			return
		}
		if it := found[0]; l.Quantity > it.MaxPerRequest || slices.ContainsFunc(req.Lines, func(rl store.RequestLine) bool { return rl.ItemID == it.ID }) {
			writeErr(w, http.StatusUnprocessableEntity, fmt.Sprintf("can add at most %d of %s", it.MaxPerRequest, l.SKU))
			return
		}
		changes.Added = append(changes.Added, swag.Line{ItemID: found[0].ID, SKU: l.SKU, Quantity: l.Quantity})
	}
	all := slices.Clone(req.Lines)
	for _, a := range changes.Added {
		all = append(all, store.RequestLine{ItemID: a.ItemID, SKU: a.SKU, Quantity: a.Quantity})
	}
	lines := make([]swag.Shipped, len(all))
	shipsWith := map[int64][]string{}
	shipping := 0
	for i, l := range all {
		if q, ok := changes.Quantities[l.SKU]; ok {
			l.Quantity = q
		}
		shipping += l.Quantity
		lines[i] = swag.Shipped{ItemID: l.ItemID, SKU: l.SKU, Quantity: l.Quantity}
		if _, seen := shipsWith[l.ItemID]; seen {
			continue
		}
		it, err := s.Store.GetItem(r.Context(), l.ItemID)
		if err != nil {
			s.serverErr(w, err)
			return
		}
		shipsWith[l.ItemID] = it.ShipsWith
	}
	if shipping == 0 {
		writeErr(w, http.StatusUnprocessableEntity, "nothing left to ship; reject the request instead")
		return
	}
	var contents []theseus.Content
	for _, c := range swag.Shipment(lines, shipsWith) {
		contents = append(contents, theseus.Content{SKU: c.SKU, Quantity: c.Quantity})
	}
	backordered := s.backorderedSKUs(r.Context(), lines)
	a := req.Address
	order, err := s.Warehouse.CreateOrder(r.Context(), theseus.OrderInput{
		RecipientEmail: req.UserEmail,
		Title:          "Hack Club swag",
		IdempotencyKey: s.Config.TheseusOrderTag + "-request-" + strconv.FormatInt(req.ID, 10),
		Tags:           []string{s.Config.TheseusOrderTag},
		Metadata:       map[string]any{"merch_request_id": req.ID},
		Address: theseus.Address{
			FirstName: a.FirstName, LastName: a.LastName, Line1: a.Line1, Line2: a.Line2, City: a.City,
			State: a.State, PostalCode: a.PostalCode, Country: a.Country, Phone: a.Phone,
		},
		Contents: contents,
	})
	if err != nil {
		s.Log.Error("theseus dispatch failed", "request_id", req.ID, "err", err)
		writeErr(w, http.StatusBadGateway, "mail.hackclub.com refused the order: "+err.Error())
		return
	}
	req, err = s.Store.MarkDispatched(r.Context(), id, order.ID, changes)
	if err != nil {
		s.respondRequest(w, nil, err)
		return
	}
	if withCosts, err := s.Store.SetWarehouseCosts(r.Context(), id, order.Costs(), order.MailedAt); err != nil {
		s.Log.Error("saving warehouse costs failed", "request_id", id, "err", err)
	} else {
		req = withCosts
	}
	warning := ""
	if len(changes.Quantities) > 0 || len(changes.Added) > 0 {
		req, warning = s.refundUnusedPostage(r, req)
	}
	changed := slices.ContainsFunc(req.Lines, func(l store.RequestLine) bool { return l.RequestedQuantity != nil })
	s.trackRequest(r, v, "request_dispatched", req, map[string]any{"changed": changed, "has_note": body.Note != "", "warning": warning != ""})
	var waiting []string
	for _, l := range req.Lines {
		if l.Quantity > 0 && backordered[l.SKU] {
			waiting = append(waiting, lineName(l))
		}
	}
	s.emailRequest(r, req, "dispatched", waiting...)
	out := []requestView{s.view(req)}
	if err := s.withEstimates(r.Context(), out, true); err != nil {
		s.Log.Error("pricing shipped request failed", "request_id", id, "err", err)
	}
	writeJSON(w, http.StatusOK, struct {
		requestView
		Warning string `json:"warning,omitempty"` // for the admin: something to fix by hand
	}{out[0], warning})
}

// adminInternalNote saves the admins' Markdown note on a request.
func (s *server) adminInternalNote(w http.ResponseWriter, r *http.Request, v viewer) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct {
		InternalNote string `json:"internal_note"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	before, err := s.Store.GetRequest(r.Context(), id)
	if err != nil {
		s.respondRequest(w, nil, err)
		return
	}
	note := strings.TrimSpace(body.InternalNote)
	req, err := s.Store.SetInternalNote(r.Context(), id, note)
	if err == nil {
		s.trackRequest(r, v, "internal_note_updated", req, map[string]any{"has_note": note != "", "chars": len(note), "from_chars": len(before.InternalNote)})
	}
	s.respondRequest(w, req, err)
}

func (s *server) adminRefresh(w http.ResponseWriter, r *http.Request, v viewer) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	req, err := s.Store.GetRequest(r.Context(), id)
	if err != nil {
		s.respond(w)(nil, err)
		return
	}
	if req.TheseusOrderID == "" {
		writeErr(w, http.StatusConflict, "request has not been dispatched")
		return
	}
	req, mailed, err := s.pullOrder(r.Context(), req)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	s.trackRequest(r, v, "tracking_updated", req, map[string]any{"carrier": req.Carrier, "has_tracking": req.TrackingNumber != "", "has_postage": mailed})
	out := []requestView{s.view(req)}
	if err := s.withEstimates(r.Context(), out, true); err != nil {
		s.serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out[0])
}

func (s *server) respond(w http.ResponseWriter) func(*store.Request, error) {
	return func(req *store.Request, err error) { s.respondRequest(w, req, err) }
}

func (s *server) respondRequest(w http.ResponseWriter, req *store.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "request not found")
	case errors.Is(err, store.ErrBadTransition):
		writeErr(w, http.StatusConflict, err.Error())
	case err != nil:
		s.serverErr(w, err)
	default:
		writeJSON(w, http.StatusOK, s.view(req))
	}
}

func (s *server) adminItems(w http.ResponseWriter, r *http.Request, v viewer) {
	items, err := s.Store.ListItems(r.Context(), false)
	if err != nil {
		s.serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func validItem(it swag.Item) string {
	switch {
	case strings.TrimSpace(it.SKU) == "" || strings.TrimSpace(it.Name) == "":
		return "sku and name are required"
	case it.MaxPerRequest < 1:
		return "max_per_request must be at least 1"
	case it.MinPerRequest > it.MaxPerRequest:
		return "min_per_request can't be more than max_per_request"
	case badQuickPicks(it) != "":
		return badQuickPicks(it)
	case it.MaxPerUser != nil && *it.MaxPerUser < 0:
		return "max_per_user can't be negative"
	case (it.WidthIn == nil) != (it.HeightIn == nil):
		return "set both width_in and height_in, or neither"
	case it.WidthIn != nil && !(*it.WidthIn > 0 && *it.WidthIn <= 120 && *it.HeightIn > 0 && *it.HeightIn <= 120):
		return "size must be between 0 and 120 inches"
	case !swag.Finishes[it.Finish]:
		return "finish must be matte, gloss, linen, uncoated or enamel"
	case it.Packs != "" && swag.Packs[it.Packs] == nil:
		return "packs must be fives, postcard or sticker"
	case it.ThicknessIn != nil && !(*it.ThicknessIn > 0 && *it.ThicknessIn <= 0.5):
		return "thickness must be between 0 and 0.5 inches"
	case it.WeightOz != nil && !(*it.WeightOz > 0 && *it.WeightOz <= 800):
		return "weight must be between 0 and 800 ounces"
	case !swag.Bindings[it.Binding]:
		return "binding must be staple, perfect or trifold"
	case it.Clip != "" && !isHTTPURL(it.Clip):
		return "clip must be an http(s) URL"
	}
	for _, u := range it.Pages {
		if !isHTTPURL(u) {
			return "pages must be http(s) URLs"
		}
	}
	for _, sku := range it.ShipsWith {
		if strings.TrimSpace(sku) == "" || sku == it.SKU {
			return "ships_with must list other warehouse SKUs"
		}
	}
	for _, p := range it.Photos {
		if msg := validPhoto(p); msg != "" {
			return msg
		}
	}
	for _, u := range it.Images {
		if !isHTTPURL(u) {
			return "images must be http(s) URLs"
		}
	}
	for _, f := range it.PrintFiles {
		switch {
		case !isHTTPURL(f.URL):
			return "print file urls must be http(s) URLs"
		case len(f.Label) > 100:
			return "a print file's label is too long"
		}
	}
	labels, skus := map[string]bool{}, map[string]bool{}
	for _, v := range it.Variants {
		switch {
		case strings.TrimSpace(v.Label) == "" || strings.TrimSpace(v.SKU) == "":
			return "every size needs a label and a warehouse SKU"
		case labels[v.Label] || skus[v.SKU]:
			return "sizes must have different labels and SKUs"
		case v.WeightOz != nil && !(*v.WeightOz > 0 && *v.WeightOz <= 800):
			return "a size's weight must be between 0 and 800 ounces"
		}
		labels[v.Label], skus[v.SKU] = true, true
	}
	return ""
}

// conflictMsg explains which SKU clashed with an existing item.
func conflictMsg(err error) string {
	if strings.Contains(err.Error(), "size of another item") {
		return err.Error()
	}
	return "an item with that SKU is already listed"
}

func (s *server) adminCreateItem(w http.ResponseWriter, r *http.Request, v viewer) {
	var it swag.Item
	if !readJSON(w, r, &it) {
		return
	}
	if msg := validItem(it); msg != "" {
		writeErr(w, http.StatusUnprocessableEntity, msg)
		return
	}
	created, err := s.Store.CreateItem(r.Context(), it)
	if errors.Is(err, store.ErrConflict) {
		writeErr(w, http.StatusConflict, conflictMsg(err))
		return
	} else if err != nil {
		s.serverErr(w, err)
		return
	}
	s.trackAdmin(r, v, "item_created", map[string]any{"item_id": created.ID, "sku": created.SKU, "visible": created.Visible})
	writeJSON(w, http.StatusCreated, created)
}

func (s *server) adminUpdateItem(w http.ResponseWriter, r *http.Request, v viewer) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var it swag.Item
	if !readJSON(w, r, &it) {
		return
	}
	it.ID = id
	if msg := validItem(it); msg != "" {
		writeErr(w, http.StatusUnprocessableEntity, msg)
		return
	}
	before, _ := s.Store.GetItem(r.Context(), id)
	upd, err := s.Store.UpdateItem(r.Context(), it)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, "item not found")
	case errors.Is(err, store.ErrConflict):
		writeErr(w, http.StatusConflict, conflictMsg(err))
	case err != nil:
		s.serverErr(w, err)
	default:
		s.trackAdmin(r, v, "item_updated", map[string]any{"item_id": upd.ID, "sku": upd.SKU, "changed": changedFields(before, upd)})
		writeJSON(w, http.StatusOK, upd)
	}
}

func validPhoto(p swag.Photo) string {
	switch {
	case !isHTTPURL(p.URL):
		return "photo urls must be http(s) URLs"
	case len(p.Caption) > 300 || len(p.Credit) > 100:
		return "photo caption or credit is too long"
	}
	return ""
}

// maxImageUpload caps the original file (print PNGs run ~9 MB); it arrives
// base64-encoded in JSON because the CSRF guard only admits application/json.
const maxImageUpload = 25 << 20

// adminUploadItemImage shrinks an uploaded image to a web-sized JPEG, stores
// it on the CDN and appends it to the item's gallery.
func (s *server) adminUploadItemImage(w http.ResponseWriter, r *http.Request, v viewer) {
	id, url, _, ok := s.uploadItemImage(w, r)
	if !ok {
		return
	}
	s.trackAdmin(r, v, "item_media_added", map[string]any{"item_id": id, "kind": "image"})
	s.respondItem(w)(s.Store.AddItemImage(r.Context(), id, url))
}

// adminUploadItemPage adds a page to the end of a booklet's flip-through.
func (s *server) adminUploadItemPage(w http.ResponseWriter, r *http.Request, v viewer) {
	id, url, _, ok := s.uploadItemImage(w, r)
	if !ok {
		return
	}
	s.trackAdmin(r, v, "item_media_added", map[string]any{"item_id": id, "kind": "page"})
	s.respondItem(w)(s.Store.AddItemPage(r.Context(), id, url))
}

// adminUploadItemPhoto adds a real-world photo (with caption and credit) to
// the item's community photos, stored like gallery images.
func (s *server) adminUploadItemPhoto(w http.ResponseWriter, r *http.Request, v viewer) {
	id, url, body, ok := s.uploadItemImage(w, r)
	if !ok {
		return
	}
	p := swag.Photo{URL: url, Caption: strings.TrimSpace(body.Caption), Credit: strings.TrimSpace(body.Credit)}
	s.trackAdmin(r, v, "item_media_added", map[string]any{"item_id": id, "kind": "photo"})
	s.respondItem(w)(s.Store.AddItemPhoto(r.Context(), id, p))
}

// maxPrintFile caps a source print file (a print-ready PDF or full-size image).
const maxPrintFile = 60 << 20

// printFileTypes are the print files people can download, by sniffed type.
var printFileTypes = map[string]string{"application/pdf": ".pdf", "image/png": ".png", "image/jpeg": ".jpg", "image/tiff": ".tif"}

// adminUploadPrintFile stores an item's original print file on the CDN as is
// (not shrunk like gallery images), for its product page's "Print your own".
func (s *server) adminUploadPrintFile(w http.ResponseWriter, r *http.Request, v viewer) {
	if s.Images == nil {
		writeErr(w, http.StatusServiceUnavailable, "uploads are off: set HACKCLUB_CDN_API_KEY")
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body imageUpload
	if !readJSONLimit(w, r, &body, maxPrintFile*4/3+4096) {
		return
	}
	label := strings.TrimSpace(body.Label)
	ctype := http.DetectContentType(body.Data)
	if bytes.HasPrefix(body.Data, []byte("II*\x00")) || bytes.HasPrefix(body.Data, []byte("MM\x00*")) {
		ctype = "image/tiff"
	}
	ext, known := printFileTypes[ctype]
	switch {
	case !known:
		writeErr(w, http.StatusUnprocessableEntity, "upload a PDF, PNG, JPEG or TIFF print file")
		return
	case len(label) > 100:
		writeErr(w, http.StatusUnprocessableEntity, "a print file's label is too long")
		return
	}
	if _, err := s.Store.GetItem(r.Context(), id); errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "item not found")
		return
	} else if err != nil {
		s.serverErr(w, err)
		return
	}
	name := strings.TrimSuffix(imageName(body.Filename), ".jpg") + ext
	url, err := s.Images.Upload(r.Context(), name, ctype, body.Data)
	if err != nil {
		s.Log.Error("cdn upload failed", "item_id", id, "err", err)
		writeErr(w, http.StatusBadGateway, "cdn.hackclub.com refused the file: "+err.Error())
		return
	}
	s.trackAdmin(r, v, "item_media_added", map[string]any{"item_id": id, "kind": "print_file"})
	s.respondItem(w)(s.Store.AddItemPrintFile(r.Context(), id, swag.PrintFile{URL: url, Label: label, Bytes: int64(len(body.Data))}))
}

type imageUpload struct {
	Filename string `json:"filename"`
	Data     []byte `json:"data"` // base64
	Caption  string `json:"caption"`
	Credit   string `json:"credit"`
	Label    string `json:"label"` // print files
}

// uploadItemImage reads an image upload for the item in the path, shrinks it to
// a web-sized JPEG and stores it on the CDN. It writes the error response itself.
func (s *server) uploadItemImage(w http.ResponseWriter, r *http.Request) (id int64, url string, body imageUpload, ok bool) {
	if s.Images == nil {
		writeErr(w, http.StatusServiceUnavailable, "image uploads are off: set HACKCLUB_CDN_API_KEY")
		return
	}
	if id, ok = pathID(w, r); !ok {
		return
	}
	ok = false
	if !readJSONLimit(w, r, &body, maxImageUpload*4/3+4096) {
		return
	}
	if msg := validPhoto(swag.Photo{URL: "https://x", Caption: body.Caption, Credit: body.Credit}); msg != "" {
		writeErr(w, http.StatusUnprocessableEntity, msg)
		return
	}
	jpg, err := imgprep.Web(body.Data)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, "upload a PNG, JPEG or WebP image")
		return
	}
	if _, err := s.Store.GetItem(r.Context(), id); errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "item not found")
		return
	} else if err != nil {
		s.serverErr(w, err)
		return
	}
	url, err = s.Images.Upload(r.Context(), imageName(body.Filename), "image/jpeg", jpg)
	if err != nil {
		s.Log.Error("cdn upload failed", "item_id", id, "err", err)
		writeErr(w, http.StatusBadGateway, "cdn.hackclub.com refused the image: "+err.Error())
		return
	}
	return id, url, body, true
}

func (s *server) respondItem(w http.ResponseWriter) func(swag.Item, error) {
	return func(it swag.Item, err error) {
		switch {
		case errors.Is(err, store.ErrNotFound):
			writeErr(w, http.StatusNotFound, "item not found")
		case err != nil:
			s.serverErr(w, err)
		default:
			writeJSON(w, http.StatusOK, it)
		}
	}
}

// imageName turns "Hackpad Poster (front).PNG" into "hackpad-poster-front.jpg".
func imageName(filename string) string {
	base := strings.TrimSuffix(filename, path.Ext(filename))
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(base) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	name := strings.TrimSuffix(b.String(), "-")
	if name == "" {
		name = "image"
	}
	return name + ".jpg"
}

func (s *server) adminSKUs(w http.ResponseWriter, r *http.Request, v viewer) {
	skus, err := s.Warehouse.ListSKUs(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"skus": skus})
}

func (s *server) adminSettings(w http.ResponseWriter, r *http.Request, v viewer) {
	st, err := s.Store.GetSettings(r.Context())
	if err != nil {
		s.serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *server) adminUpdateSettings(w http.ResponseWriter, r *http.Request, v viewer) {
	var st swag.Settings
	if !readJSON(w, r, &st) {
		return
	}
	if st.InternationalShippingFeeCents < 0 {
		writeErr(w, http.StatusUnprocessableEntity, "limits can't be negative")
		return
	}
	before, _ := s.Store.GetSettings(r.Context())
	if err := s.Store.UpdateSettings(r.Context(), st); err != nil {
		s.serverErr(w, err)
		return
	}
	s.trackAdmin(r, v, "settings_updated", map[string]any{"changed": changedFields(before, st)})
	s.adminSettings(w, r, v)
}

func (s *server) adminSubmissions(w http.ResponseWriter, r *http.Request, v viewer) {
	subs, err := s.Store.ListSubmissions(r.Context())
	if err != nil {
		s.serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"submissions": subs})
}

var submissionStatuses = map[string]bool{"submitted": true, "approved": true, "printing": true, "stocked": true, "rejected": true}

func (s *server) adminUpdateSubmission(w http.ResponseWriter, r *http.Request, v viewer) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var body struct {
		Status    string `json:"status"`
		AdminNote string `json:"admin_note"`
		SKU       string `json:"sku"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if !submissionStatuses[body.Status] {
		writeErr(w, http.StatusUnprocessableEntity, "unknown status")
		return
	}
	from := ""
	if prev, err := s.Store.GetSubmission(r.Context(), id); err == nil {
		from = prev.Status
	}
	sub, err := s.Store.UpdateSubmission(r.Context(), id, body.Status, body.AdminNote, strings.TrimSpace(body.SKU))
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "submission not found")
		return
	} else if err != nil {
		s.serverErr(w, err)
		return
	}
	s.trackAdmin(r, v, "submission_updated", map[string]any{"submission_id": sub.ID, "from": from, "to": sub.Status, "kind": sub.Kind, "has_sku": sub.SKU != ""})
	writeJSON(w, http.StatusOK, sub)
}

// ---- static PWA ----

func (s *server) static(w http.ResponseWriter, r *http.Request) {
	if s.Static == nil {
		http.NotFound(w, r)
		return
	}
	p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if p == "" {
		p = "index.html"
	}
	if st, err := fs.Stat(s.Static, p); err != nil || st.IsDir() {
		p = "index.html" // SPA fallback: client-side router handles the path
	}
	switch {
	case p == "index.html" || p == "service-worker.js" || p == "sw.js" || strings.HasSuffix(p, ".webmanifest"):
		w.Header().Set("Cache-Control", "no-cache")
	case strings.HasPrefix(p, "_app/immutable/"):
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	if p == "index.html" {
		s.serveIndex(w, r)
		return
	}
	s.serveStatic(w, r, p)
}

// serveStatic sends the build's brotli or gzip copy of p (adapter-static precompress) when the
// browser takes it: the pod has too little CPU to brotli big JS on the fly (JSON: gzipJSON).
func (s *server) serveStatic(w http.ResponseWriter, r *http.Request, p string) {
	accept := acceptedEncodings(r.Header.Get("Accept-Encoding"))
	for _, e := range []struct{ ext, name string }{{".br", "br"}, {".gz", "gzip"}} {
		if _, err := fs.Stat(s.Static, p+e.ext); err != nil {
			continue
		}
		w.Header().Set("Vary", "Accept-Encoding")
		if !accept[e.name] {
			continue
		}
		if ct := mime.TypeByExtension(path.Ext(p)); ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		w.Header().Set("Content-Encoding", e.name)
		http.ServeFileFS(w, r, s.Static, p+e.ext)
		return
	}
	http.ServeFileFS(w, r, s.Static, p)
}

// acceptedEncodings reads an Accept-Encoding header, leaving out codings refused with q=0.
func acceptedEncodings(h string) map[string]bool {
	ok := map[string]bool{}
	for _, part := range strings.Split(h, ",") {
		name, params, _ := strings.Cut(part, ";")
		if q, isQ := strings.CutPrefix(strings.ReplaceAll(params, " ", ""), "q="); isQ {
			if f, err := strconv.ParseFloat(q, 64); err == nil && f == 0 {
				continue
			}
		}
		ok[strings.ToLower(strings.TrimSpace(name))] = true
	}
	return ok
}

// ---- helpers ----

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeErr(w, http.StatusBadRequest, "bad id")
		return 0, false
	}
	return id, true
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	return readJSONLimit(w, r, v, 1<<20)
}

func readJSONLimit(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	if err := dec.Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func (s *server) serverErr(w http.ResponseWriter, err error) {
	s.Log.Error("internal error", "err", err)
	if sw, ok := w.(*statusWriter); ok {
		sw.err = err
	}
	writeErr(w, http.StatusInternalServerError, "something went wrong")
}

// badQuickPicks says what's wrong with an item's one-tap amounts, or "" if
// nothing: each must be orderable (within the limits, a real pack size) and
// they must go up.
func badQuickPicks(it swag.Item) string {
	for i, q := range it.QuickPicks {
		switch {
		case q < max(it.MinPerRequest, 1) || q > it.MaxPerRequest:
			return "quick pick " + strconv.Itoa(q) + " is outside the item's limits"
		case it.Packs != "" && !swag.PackQuantity(it.Packs, q):
			return "quick pick " + strconv.Itoa(q) + " isn't a pack size (" + swag.PackSizes(it.Packs) + ")"
		case i > 0 && q <= it.QuickPicks[i-1]:
			return "quick picks must go up"
		}
	}
	return ""
}
