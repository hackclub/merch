package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/hackclub/merch/internal/store"
	"github.com/hackclub/merch/internal/swag"
)

// Usage analytics: what people do on the site goes to usage_events, for agents
// and admins improving it (AGENTS.md, "Usage analytics"). The browser sends its
// own events (page views, time on page, clicks, cart changes) to /api/events;
// handlers record the rest (shipping estimates, requests, sign-ins) with track.

const (
	visitorCookie = "zmr_visitor"
	visitorTTL    = 400 * 24 * time.Hour // the longest browsers keep a cookie
	maxEventBatch = 200
	maxPropsBytes = 4 << 10
	maxEventsBody = 512 << 10
)

var (
	eventName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
	idChars   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
)

type visitorKey struct{}

// visitor gives every browser a random id in a cookie, so one person's events
// (from before and after they sign in) can be put together.
func (s *server) visitor(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Not on health checks or immutable assets (they're cached).
		if r.URL.Path == "/healthz" || strings.HasPrefix(r.URL.Path, "/_app/") {
			next.ServeHTTP(w, r)
			return
		}
		var id string
		if c, err := r.Cookie(visitorCookie); err == nil && idChars.MatchString(c.Value) {
			id = c.Value
		} else {
			b := make([]byte, 16)
			rand.Read(b)
			id = base64.RawURLEncoding.EncodeToString(b)
			http.SetCookie(w, &http.Cookie{
				Name: visitorCookie, Value: id, Path: "/", MaxAge: int(visitorTTL.Seconds()),
				HttpOnly: true, Secure: s.secure(r), SameSite: http.SameSiteLaxMode,
			})
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), visitorKey{}, id)))
	})
}

func visitorOf(r *http.Request) string {
	id, _ := r.Context().Value(visitorKey{}).(string)
	return id
}

// visitOf is the tab's visit id, which the app sends with every API call.
func visitOf(r *http.Request) string {
	if v := r.Header.Get("X-Merch-Visit"); idChars.MatchString(v) {
		return v
	}
	return ""
}

// pagePath is the path of the app page that made an API call, from its Referer.
func pagePath(r *http.Request) string {
	u, err := url.Parse(r.Referer())
	if err != nil || u.Host == "" {
		return ""
	}
	if u.Host != r.Host && u.Host != strings.Split(r.Header.Get("X-Forwarded-Host"), ",")[0] {
		return ""
	}
	return clip(u.Path, 512)
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// track records a server-side event. It never fails the request.
func (s *server) track(r *http.Request, u *store.User, name string, props map[string]any) {
	b, err := json.Marshal(props)
	if err != nil {
		s.Log.Warn("usage event not encodable", "name", name, "err", err)
		return
	}
	e := store.Event{Source: "server", Name: name, Visitor: visitorOf(r), Visit: visitOf(r), Path: pagePath(r), Props: b}
	if u != nil {
		e.UserID = &u.ID
	}
	if err := s.Store.RecordEvents(r.Context(), []store.Event{e}); err != nil {
		s.Log.Warn("usage event not saved", "name", name, "err", err)
	}
}

// clientEvents saves a batch of the browser's events. Anyone can send them.
// Malformed events are dropped one by one, so one bad event doesn't lose the
// rest. Times move onto the server's clock: an event t ms before the batch was
// sent happened that long before it arrived.
func (s *server) clientEvents(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Visit  string `json:"visit"`
		Sent   int64  `json:"sent"`
		Events []struct {
			Name  string          `json:"name"`
			Path  string          `json:"path"`
			T     int64           `json:"t"`
			Props json.RawMessage `json:"props"`
		} `json:"events"`
	}
	if !readJSONLimit(w, r, &body, maxEventsBody) {
		return
	}
	if len(body.Events) > maxEventBatch {
		writeErr(w, http.StatusRequestEntityTooLarge, "too many events in one batch")
		return
	}
	now := s.Now()
	visit := body.Visit
	if !idChars.MatchString(visit) {
		visit = visitOf(r)
	}
	var uid *int64
	if u := s.currentUser(r); u != nil {
		uid = &u.ID
	}
	var evs []store.Event
	for _, in := range body.Events {
		props := bytes.TrimSpace(in.Props)
		if len(props) == 0 || string(props) == "null" {
			props = []byte(`{}`)
		}
		if !eventName.MatchString(in.Name) || props[0] != '{' || len(props) > maxPropsBytes {
			continue
		}
		at := now
		if lag := body.Sent - in.T; body.Sent > 0 && in.T > 0 && lag >= 0 && lag < (24*time.Hour).Milliseconds() {
			at = now.Add(-time.Duration(lag) * time.Millisecond)
		}
		if in.Name == "visit_start" {
			var m map[string]any
			if json.Unmarshal(props, &m) == nil {
				m["ua"] = clip(r.UserAgent(), 300)
				props, _ = json.Marshal(m)
			}
		}
		evs = append(evs, store.Event{
			At: at, Source: "client", Name: in.Name, Visitor: visitorOf(r), Visit: visit, UserID: uid,
			Path: clip(in.Path, 512), Props: props,
		})
	}
	if len(evs) > 0 {
		if err := s.Store.RecordEvents(r.Context(), evs); err != nil {
			s.serverErr(w, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// estimateProps is what a shipping estimate logs: the request's contents and
// country (never the address), how it packs, and what we told them.
func estimateProps(country string, lines []swag.Line, p swag.Packing, e swag.Estimate) map[string]any {
	return map[string]any{
		"country": country, "lines": lines,
		"package": p.Package.Name, "contents_oz": p.ContentsOz, "weight_oz": p.WeightOz, "unknown_weight": p.UnknownWeight,
		"class": e.Class, "service": e.Service, "handling_days": e.HandlingDays, "transit_days": e.TransitDays,
		"arrive_from": e.ArriveFrom, "arrive_by": e.ArriveBy, "fee_cents": e.FeeCents,
		"postage_cents": e.PostageCents, "postage_typical_cents": e.PostageTypicalCents,
		"transit_basis": e.TransitBasis, "transit_samples": e.TransitSamples, "postage_samples": e.PostageSamples,
	}
}

func itemCount(lines []swag.Line) int {
	n := 0
	for _, l := range lines {
		n += l.Quantity
	}
	return n
}
