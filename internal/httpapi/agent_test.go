package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hackclub/merch/internal/auth"
	"github.com/hackclub/merch/internal/store"
	"github.com/hackclub/merch/internal/swag"
	"github.com/hackclub/merch/internal/theseus"
)

// keyClient calls the API with an agent's key, the way a scheduled agent does.
func (h *harness) keyClient(tok string) *http.Client {
	return &http.Client{Transport: bearerTransport{tok}}
}

type bearerTransport struct{ tok string }

func (b bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r.Header.Set("Authorization", "Bearer "+b.tok)
	return http.DefaultTransport.RoundTrip(r)
}

type createdKey struct {
	Key   store.APIKey `json:"key"`
	Token string       `json:"token"`
}

func (h *harness) mkKey(name string) createdKey {
	h.t.Helper()
	var k createdKey
	if code := h.do(h.client(adminEmail), "POST", "/api/admin/api-keys", map[string]any{"name": name}, &k); code != 201 {
		h.t.Fatalf("create key = %d", code)
	}
	return k
}

func TestAdminManagesAPIKeys(t *testing.T) {
	h := newHarness(t)
	admin := h.client(adminEmail)
	if code := h.do(h.client(userEmail), "POST", "/api/admin/api-keys", map[string]any{"name": "x"}, nil); code != 403 {
		t.Fatalf("non-admin creates key = %d", code)
	}
	if code := h.do(admin, "POST", "/api/admin/api-keys", map[string]any{"name": " "}, nil); code != 422 {
		t.Fatalf("unnamed key = %d", code)
	}
	k := h.mkKey("Review agent")
	if !strings.HasPrefix(k.Token, "merch_") || k.Key.Name != "Review agent" {
		t.Fatalf("created %+v", k)
	}
	agent := h.keyClient(k.Token)
	if code := h.do(agent, "GET", "/api/agent/requests", nil, nil); code != 200 {
		t.Fatalf("agent list = %d", code)
	}
	// The token is shown once; the list never has it.
	resp, _ := admin.Get(h.srv.URL + "/api/admin/api-keys")
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || strings.Contains(string(b), k.Token) || !strings.Contains(string(b), "Review agent") || !strings.Contains(string(b), "last_used_at\":\"") {
		t.Fatalf("list keys = %d %s", resp.StatusCode, b)
	}
	// A key opens the agent API only: not admin routes, not key management.
	for _, p := range []string{"/api/admin/requests", "/api/admin/api-keys"} {
		if code := h.do(agent, "GET", p, nil, nil); code != 401 {
			t.Errorf("agent GET %s = %d, want 401", p, code)
		}
	}
	if code := h.do(agent, "POST", "/api/admin/api-keys", map[string]any{"name": "more"}, nil); code != 401 {
		t.Errorf("agent minted a key: %d", code)
	}
	if code := h.do(admin, "POST", fmt.Sprintf("/api/admin/api-keys/%d/revoke", k.Key.ID), map[string]any{}, nil); code != 200 {
		t.Fatalf("revoke = %d", code)
	}
	if code := h.do(agent, "GET", "/api/agent/requests", nil, nil); code != 401 {
		t.Fatalf("revoked key = %d", code)
	}
	for _, tok := range []string{"", "merch_nope", k.Token + "x"} {
		if code := h.do(h.keyClient(tok), "GET", "/api/agent/requests", nil, nil); code != 401 {
			t.Errorf("token %q = %d", tok, code)
		}
	}
	// A signed-in admin's cookie isn't a key.
	if code := h.do(admin, "GET", "/api/agent/requests", nil, nil); code != 401 {
		t.Errorf("cookie on agent API = %d", code)
	}
	if evs := h.events("api_key_created"); len(evs) != 1 || props(t, evs[0])["api_key_id"] != float64(k.Key.ID) {
		t.Errorf("api_key_created = %+v", evs)
	}
	if evs := h.events("api_key_revoked"); len(evs) != 1 || props(t, evs[0])["api_key_id"] != float64(k.Key.ID) {
		t.Errorf("api_key_revoked = %+v", evs)
	}
	for _, e := range h.events("") {
		if strings.Contains(string(e.Props), k.Token[6:]) {
			t.Errorf("%s logged the token", e.Name)
		}
	}
}

// A key acts for the admin who made it, so it stops when they stop being one.
func TestAPIKeyEndsWithItsAdmin(t *testing.T) {
	h := newHarness(t)
	k := h.mkKey("agent")
	h.cfg.AdminEmails = []string{"someone-else@hackclub.com"}
	if code := h.do(h.keyClient(k.Token), "GET", "/api/agent/requests", nil, nil); code != 403 {
		t.Fatalf("ex-admin's key = %d", code)
	}
}

type agentRequest struct {
	requestView
	Requester   store.Requester  `json:"requester"`
	History     []*store.Request `json:"history"`
	SameAddress []*store.Request `json:"same_address"`
}

func TestAgentReviewsRequests(t *testing.T) {
	h := newHarness(t)
	it := h.mkItem("Sti/A", true)
	agent := h.keyClient(h.mkKey("Review agent").Token)
	mk := func(email string, n int, purpose, note string) store.Request {
		t.Helper()
		var r store.Request
		if code := h.do(h.client(email), "POST", "/api/requests", map[string]any{"purpose": purpose, "note": note, "address": usAddr, "lines": []swag.Line{{ItemID: it.ID, Quantity: n}}}, &r); code != 201 {
			t.Fatalf("create = %d", code)
		}
		return r
	}
	first := mk(userEmail, 3, "personal", "")
	reason := "Club fair on Sept 12.\nAbout 40 people."
	twin := mk("twin@example.com", 1, "event", reason) // another account, same house

	var list struct{ Requests []requestView }
	if code := h.do(agent, "GET", "/api/agent/requests?status=pending", nil, &list); code != 200 || len(list.Requests) != 2 || list.Requests[0].Costs == nil {
		t.Fatalf("list = %d %+v", code, list)
	}
	// What the requester wrote reaches the agent word for word.
	if list.Requests[0].ID != twin.ID || list.Requests[0].Note != reason || list.Requests[1].Note != "" {
		t.Errorf("notes = %q %q", list.Requests[0].Note, list.Requests[1].Note)
	}

	var got agentRequest
	if code := h.do(agent, "GET", fmt.Sprintf("/api/agent/requests/%d", first.ID), nil, &got); code != 200 {
		t.Fatalf("get = %d", code)
	}
	if got.ID != first.ID || got.Requester.Email != userEmail || got.Requester.CreatedAt.IsZero() || got.Costs == nil {
		t.Errorf("request = %+v", got)
	}
	if len(got.History) != 0 || len(got.SameAddress) != 1 || got.SameAddress[0].ID != twin.ID || got.SameAddress[0].UserEmail != "twin@example.com" || got.SameAddress[0].Note != reason {
		t.Errorf("history %+v same address %+v", got.History, got.SameAddress)
	}
	if code := h.do(agent, "GET", "/api/agent/requests/99999", nil, nil); code != 404 {
		t.Errorf("missing request = %d", code)
	}

	// Notes add to the internal note, signed, and never replace what an admin wrote.
	note := func(id int64, text string) store.Request {
		t.Helper()
		var r store.Request
		if code := h.do(agent, "POST", fmt.Sprintf("/api/agent/requests/%d/notes", id), map[string]any{"note": text}, &r); code != 200 {
			t.Fatalf("note = %d", code)
		}
		return r
	}
	if _, err := h.store.SetInternalNote(t.Context(), first.ID, "Zach: known club leader"); err != nil {
		t.Fatal(err)
	}
	r := note(first.ID, "Same address as #"+fmt.Sprint(twin.ID)+"; looks like siblings.")
	if !strings.HasPrefix(r.InternalNote, "Zach: known club leader\n\n**Review agent**") || !strings.Contains(r.InternalNote, "looks like siblings.") {
		t.Errorf("internal note = %q", r.InternalNote)
	}
	if code := h.do(agent, "POST", fmt.Sprintf("/api/agent/requests/%d/notes", first.ID), map[string]any{"note": "  "}, nil); code != 422 {
		t.Errorf("empty note = %d", code)
	}

	// Ship fewer than asked, with a note for the requester.
	ship := map[string]any{"lines": []map[string]any{{"sku": "Sti/A", "quantity": 2}}, "note": "Sending 2 so there are enough to go around."}
	if code := h.do(agent, "POST", fmt.Sprintf("/api/agent/requests/%d/dispatch", first.ID), ship, &r); code != 200 || r.Status != swag.StatusDispatched {
		t.Fatalf("dispatch = %d %+v", code, r)
	}
	if len(h.wh.orders) != 1 || h.wh.orders[0].Contents[0].Quantity != 2 {
		t.Fatalf("warehouse orders = %+v", h.wh.orders)
	}
	// The agent pulls tracking for what it shipped.
	if code := h.do(agent, "POST", fmt.Sprintf("/api/agent/requests/%d/refresh", first.ID), map[string]any{}, &r); code != 200 || r.TrackingNumber != "9400" || r.Carrier != "USPS" {
		t.Fatalf("refresh = %d %+v", code, r)
	}
	if code := h.do(agent, "POST", fmt.Sprintf("/api/agent/requests/%d/refresh", twin.ID), map[string]any{}, nil); code != 409 {
		t.Errorf("refresh before dispatch = %d", code)
	}
	if code := h.do(agent, "POST", fmt.Sprintf("/api/agent/requests/%d/reject", twin.ID), map[string]any{"note": "One package per household."}, &r); code != 200 || r.Status != swag.StatusRejected {
		t.Fatalf("reject = %d %+v", code, r)
	}

	// The requester's history shows up on their next request.
	again := mk(userEmail, 1, "personal", "")
	h.do(agent, "GET", fmt.Sprintf("/api/agent/requests/%d", again.ID), nil, &got)
	if len(got.History) != 1 || got.History[0].ID != first.ID || got.History[0].Lines[0].Quantity != 2 {
		t.Errorf("history = %+v", got.History)
	}

	// Every decision says which key made it, and no note text is logged.
	for _, name := range []string{"request_dispatched", "request_rejected", "internal_note_updated"} {
		evs := h.events(name)
		if len(evs) != 1 {
			t.Errorf("%s events = %d", name, len(evs))
			continue
		}
		if p := props(t, evs[0]); p["api_key_id"] == nil || p["admin_id"] == nil {
			t.Errorf("%s props = %v", name, p)
		}
	}
	for _, e := range h.events("") {
		if s := string(e.Props); strings.Contains(s, "siblings") || strings.Contains(s, "household") || strings.Contains(s, "go around") {
			t.Errorf("%s logged a note: %s", e.Name, s)
		}
	}
}

func TestAgentAPIDocs(t *testing.T) {
	h := newHarness(t)
	resp, err := http.Get(h.srv.URL + "/api/agent")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/markdown") {
		t.Fatalf("docs = %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	for _, want := range []string{"Authorization: Bearer", "/api/agent/requests/{id}/dispatch", "/api/agent/requests/{id}/reject", "/api/agent/requests/{id}/notes", "/api/agent/requests/{id}/refresh", "same_address"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("docs don't mention %q", want)
		}
	}
}

// Shipped requests keep their cost: the warehouse's charges (labor, postage)
// where it gave them, else the estimate, so the review agent can report what
// it shipped. Items are always at unit cost: the warehouse never bills for them,
// and its contents_cost (0.01 in the fake) is a stale per-SKU figure.
func TestAgentSeesShippedCosts(t *testing.T) {
	h := newHarness(t)
	it := h.mkItem("Sti/A", true)
	agent := h.keyClient(h.mkKey("Review agent").Token)
	var req store.Request
	if code := h.do(h.client(userEmail), "POST", "/api/requests", map[string]any{"purpose": "personal", "address": usAddr, "lines": []swag.Line{{ItemID: it.ID, Quantity: 2}}}, &req); code != 201 {
		t.Fatalf("create = %d", code)
	}
	var shipped requestView
	if code := h.do(agent, "POST", fmt.Sprintf("/api/agent/requests/%d/dispatch", req.ID), map[string]any{}, &shipped); code != 200 {
		t.Fatalf("dispatch = %d", code)
	}
	if c := shipped.Costs; c == nil || c.ItemsCents != 50 || c.LaborCents != 220 || c.TotalCents != c.ItemsCents+c.LaborCents+c.PackingCents+c.PostageCents || fmt.Sprint(shipped.CostsActual) != "[labor]" {
		t.Fatalf("dispatch costs = %+v %v", c, shipped.CostsActual)
	}
	var got agentRequest
	h.do(agent, "GET", fmt.Sprintf("/api/agent/requests/%d", req.ID), nil, &got)
	if got.Costs == nil || got.Costs.TotalCents != shipped.Costs.TotalCents {
		t.Fatalf("get costs = %+v", got.Costs)
	}
	var list struct{ Requests []requestView }
	h.do(agent, "GET", "/api/agent/requests?status=dispatched", nil, &list)
	if len(list.Requests) != 1 || list.Requests[0].Costs == nil {
		t.Fatalf("list = %+v", list)
	}
	var refreshed requestView
	if code := h.do(agent, "POST", fmt.Sprintf("/api/agent/requests/%d/refresh", req.ID), map[string]any{}, &refreshed); code != 200 || refreshed.Costs == nil || fmt.Sprint(refreshed.CostsActual) != "[labor postage]" {
		t.Fatalf("refresh = %d %+v %v", code, refreshed.Costs, refreshed.CostsActual)
	}
	// Requesters never see costs.
	var mine struct{ Requests []map[string]any }
	h.do(h.client(userEmail), "GET", "/api/requests", nil, &mine)
	if _, ok := mine.Requests[0]["costs"]; ok {
		t.Fatalf("requester sees costs: %+v", mine.Requests[0])
	}
}

type bulkRefreshReply struct {
	Checked, Updated, Mailed, Failed, Left int
	Next                                   *int64  `json:"next"`
	TrackingIDs                            []int64 `json:"tracking_ids"`
	MailedIDs                              []int64 `json:"mailed_ids"`
}

func TestAgentBulkRefresh(t *testing.T) {
	for _, mode := range []string{"unmailed", "mailed", "failed"} {
		t.Run(mode, func(t *testing.T) {
			h := newHarness(t)
			it := h.mkItem("Sti/A", true)
			u, err := h.store.UpsertUser(t.Context(), auth.Identity{ID: "bulk-user", Email: userEmail})
			if err != nil {
				t.Fatal(err)
			}
			ids := make([]int64, 69)
			for i := range ids {
				req, err := h.store.CreateRequest(t.Context(), store.NewRequest{UserID: u.ID, Address: usAddr, Status: swag.StatusPending, Lines: []swag.Line{{ItemID: it.ID, Quantity: 1}}}, func(map[int64]int) error { return nil })
				if err != nil {
					t.Fatal(err)
				}
				ids[i] = req.ID
			}
			// Dispatch in reverse order: pagination must follow IDs, not dispatch time.
			for i := len(ids) - 1; i >= 0; i-- {
				if _, err := h.store.MarkDispatched(t.Context(), ids[i], fmt.Sprint(ids[i]), store.ShipChanges{}); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "mailed" {
				// A changed tracking number is reported; an unchanged one isn't.
				if _, err := h.store.SetTracking(t.Context(), ids[0], "old", "USPS"); err != nil {
					t.Fatal(err)
				}
				if _, err := h.store.SetTracking(t.Context(), ids[1], "9400", "USPS"); err != nil {
					t.Fatal(err)
				}
			}
			var called []string
			fail := mode == "failed"
			h.wh.getOrder = func(id string) (*theseus.Order, error) {
				called = append(called, id)
				if fail && id == fmt.Sprint(ids[59]) {
					return nil, errors.New("warehouse unavailable")
				}
				o := &theseus.Order{ID: id, LaborCost: 2.2}
				if mode == "mailed" {
					now := time.Now()
					o.TrackingNumber, o.Carrier, o.PostageCost, o.MailedAt = "9400", "USPS", "4.71", &now
				}
				return o, nil
			}
			k := h.mkKey("Bulk reviewer")
			agent := h.keyClient(k.Token)
			path := "/api/agent/requests/refresh"
			if code := h.do(&http.Client{}, "POST", path, map[string]any{}, nil); code != 401 {
				t.Fatalf("no key = %d", code)
			}
			if code := h.do(agent, "POST", "/api/admin/requests/refresh", map[string]any{}, nil); code != 401 {
				t.Fatalf("admin route = %d", code)
			}
			for _, after := range []any{-1, "bad", 1.5} {
				if code := h.do(agent, "POST", path, map[string]any{"after": after}, nil); code != 400 {
					t.Fatalf("after %v = %d", after, code)
				}
			}
			if len(called) != 0 {
				t.Fatal("unauthorized or invalid call reached warehouse")
			}
			var first, second bulkRefreshReply
			if code := h.do(agent, "POST", path, map[string]any{}, &first); code != 200 {
				t.Fatalf("bulk = %d", code)
			}
			wantFailed, wantMailed := 0, 0
			if mode == "failed" {
				wantFailed = 1
			}
			if mode == "mailed" {
				wantMailed = 60
			}
			if first.Checked != 60 || first.Updated != 60-wantFailed || first.Mailed != wantMailed || first.Failed != wantFailed || first.Left != 9 || first.Next == nil || *first.Next != ids[59] {
				t.Fatalf("first = %+v", first)
			}
			if first.TrackingIDs == nil || first.MailedIDs == nil {
				t.Fatal("ID arrays must be arrays, not null")
			}
			if mode == "mailed" {
				wantTracking := append([]int64{ids[0]}, ids[2:60]...)
				if !slices.Equal(first.TrackingIDs, wantTracking) || !slices.Equal(first.MailedIDs, ids[:60]) {
					t.Fatalf("changed IDs = %+v", first)
				}
				got, err := h.store.GetRequest(t.Context(), ids[0])
				if err != nil || got.TrackingNumber != "9400" || got.MailedAt == nil || got.WarehouseCosts.PostageCents == nil || *got.WarehouseCosts.PostageCents != 471 {
					t.Fatalf("saved = %+v, %v", got, err)
				}
			} else if len(first.TrackingIDs) != 0 || len(first.MailedIDs) != 0 {
				t.Fatalf("unmailed IDs = %+v", first)
			}
			evs := h.events("requests_refreshed")
			if len(evs) != 1 {
				t.Fatalf("events = %+v", evs)
			}
			p := props(t, evs[0])
			if p["api_key_id"] != float64(k.Key.ID) || p["checked"] != float64(60) || p["updated"] != float64(first.Updated) || p["mailed"] != float64(first.Mailed) || p["failed"] != float64(first.Failed) || p["left"] != float64(9) {
				t.Fatalf("event = %+v", p)
			}
			if code := h.do(agent, "POST", path, map[string]any{"after": *first.Next}, &second); code != 200 {
				t.Fatalf("continuation = %d", code)
			}
			if second.Checked != 9 || second.Updated != 9 || second.Left != 0 || second.Next != nil {
				t.Fatalf("second = %+v", second)
			}
			if mode == "mailed" && (!slices.Equal(second.TrackingIDs, ids[60:]) || !slices.Equal(second.MailedIDs, ids[60:])) {
				t.Fatalf("continuation IDs = %+v", second)
			}
			if len(called) != 69 {
				t.Fatalf("warehouse calls = %d", len(called))
			}
			for i, id := range ids {
				if called[i] != fmt.Sprint(id) {
					t.Fatalf("call %d = %s, want %d", i, called[i], id)
				}
			}
			if mode == "failed" {
				fail = false
				var fresh bulkRefreshReply
				// Empty bodies remain accepted when sent as JSON, like other API calls.
				req, err := http.NewRequest("POST", h.srv.URL+path, nil)
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Content-Type", "application/json")
				resp, err := agent.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				if err := json.NewDecoder(resp.Body).Decode(&fresh); err != nil {
					t.Fatal(err)
				}
				if resp.StatusCode != 200 || fresh.Checked != 60 || fresh.Failed != 0 || fresh.Updated != 60 {
					t.Fatalf("fresh run = %d %+v", resp.StatusCode, fresh)
				}
				if called[69+59] != fmt.Sprint(ids[59]) {
					t.Fatal("fresh run didn't retry failed request")
				}
			}
		})
	}
}
