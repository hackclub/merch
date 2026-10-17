package httpapi

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/hackclub/merch/internal/store"
	"github.com/hackclub/merch/internal/swag"
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
	for _, want := range []string{"Authorization: Bearer", "/api/agent/requests/{id}/dispatch", "/api/agent/requests/{id}/reject", "/api/agent/requests/{id}/notes", "same_address"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("docs don't mention %q", want)
		}
	}
}
