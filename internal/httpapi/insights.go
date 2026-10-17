package httpapi

import (
	"encoding/json"
	"math"
	"net/http"
	"reflect"
	"time"

	"github.com/hackclub/merch/internal/store"
)

// Admin actions as usage events (AGENTS.md, "Usage analytics"): how long
// requests wait, what ships short or gets turned down, and when the catalog
// and limits changed, so an agent can line up what people do with what we did.
// Request events belong to the requester (like payment_completed); admin_id
// says who acted. Notes are never logged, only whether there was one.

// requestFacts describes a request without saying who or where (beyond the country).
func (s *server) requestFacts(req *store.Request) map[string]any {
	asked, shipped := 0, 0
	for _, l := range req.Lines {
		shipped += l.Quantity
		if l.RequestedQuantity != nil {
			asked += *l.RequestedQuantity
		} else {
			asked += l.Quantity
		}
	}
	p := map[string]any{
		"request_id": req.ID, "country": req.Address.Country, "purpose": req.Purpose, "source": req.Source,
		"lines": len(req.Lines), "items_requested": asked, "items_shipped": shipped,
		"fee_cents": req.ShippingFeeCents, "wait_h": hoursSince(s.Now(), req.CreatedAt),
	}
	if req.PaidAt != nil {
		p["since_paid_h"] = hoursSince(s.Now(), *req.PaidAt)
	}
	if req.RefundedCents > 0 {
		p["refunded_cents"] = req.RefundedCents
	}
	return p
}

func hoursSince(now, t time.Time) float64 {
	return math.Round(now.Sub(t).Hours()*10) / 10
}

// trackRequest records what an admin did to a request.
func (s *server) trackRequest(r *http.Request, v viewer, name string, req *store.Request, extra map[string]any) {
	p := s.requestFacts(req)
	for k, x := range extra {
		p[k] = x
	}
	if v.user != nil {
		p["admin_id"] = v.user.ID
	}
	if v.key != nil {
		p["api_key_id"] = v.key.ID
	}
	s.track(r, &store.User{ID: req.UserID}, name, p)
}

// trackAdmin records an admin's change to the catalog, settings or submissions.
func (s *server) trackAdmin(r *http.Request, v viewer, name string, p map[string]any) {
	if v.key != nil {
		p["api_key_id"] = v.key.ID
	}
	s.track(r, v.user, name, p)
}

// changedFields compares two values' JSON fields: {field: {from, to}} for each
// that changed. Lists give their lengths and long text its first 80 characters,
// so photo credits and descriptions don't fill the table.
func changedFields(before, after any) map[string]any {
	a, b := jsonFields(before), jsonFields(after)
	out := map[string]any{}
	for k, to := range b {
		from := a[k]
		if k == "id" || k == "updated_at" || k == "created_at" || reflect.DeepEqual(from, to) {
			continue
		}
		out[k] = map[string]any{"from": summarize(from), "to": summarize(to)}
	}
	return out
}

func jsonFields(v any) map[string]any {
	b, _ := json.Marshal(v)
	m := map[string]any{}
	json.Unmarshal(b, &m)
	return m
}

func summarize(v any) any {
	switch x := v.(type) {
	case []any:
		return len(x)
	case map[string]any:
		return "…"
	case string:
		return clip(x, 80)
	}
	return v
}
