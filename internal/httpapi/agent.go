package httpapi

import (
	_ "embed"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/hackclub/merch/internal/store"
)

// The agent API (/api/agent/*): a scheduled agent reviews pending requests
// with an API key an admin made. The key acts for that admin, but only here:
// it can read requests and their requesters, add notes, ship and reject. It
// can't touch items, settings or keys. Docs: GET /api/agent (agent_api.md).

//go:embed agent_api.md
var agentDocs string

// requireAgent lets in a live key whose admin is still an admin.
func (s *server) requireAgent(h handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || !strings.HasPrefix(tok, "merch_") {
			writeErr(w, http.StatusUnauthorized, "send your API key as Authorization: Bearer merch_…")
			return
		}
		k, u, err := s.Store.APIKeyByToken(r.Context(), strings.TrimSpace(tok))
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, http.StatusUnauthorized, "this API key doesn't exist or was revoked")
			return
		} else if err != nil {
			s.serverErr(w, err)
			return
		}
		if !s.Config.IsAdminEmail(u.Email) {
			writeErr(w, http.StatusForbidden, "the admin who made this API key isn't an admin anymore")
			return
		}
		h(w, r, viewer{user: u, roles: roles{Admin: true, Author: true}, key: k})
	})
}

func (s *server) agentDocs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Write([]byte(strings.ReplaceAll(agentDocs, "{BASE_URL}", s.origin(r))))
}

// agentRequest is one request with what a reviewer needs to judge it: who
// asked, everything they asked for before, and other people's requests to the
// same address.
func (s *server) agentRequest(w http.ResponseWriter, r *http.Request, v viewer) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	req, err := s.Store.GetRequest(r.Context(), id)
	if err != nil {
		s.respondRequest(w, nil, err)
		return
	}
	s.syncPayments(r, []*store.Request{req})
	out := []requestView{s.view(req)}
	if err := s.withEstimates(r.Context(), out, true); err != nil {
		s.serverErr(w, err)
		return
	}
	who, err := s.Store.GetRequester(r.Context(), req.UserID)
	if err != nil {
		s.serverErr(w, err)
		return
	}
	mine, err := s.Store.ListRequestsByUser(r.Context(), req.UserID)
	if err != nil {
		s.serverErr(w, err)
		return
	}
	history := []*store.Request{}
	for _, o := range mine {
		if o.ID != req.ID {
			history = append(history, o)
		}
	}
	same, err := s.Store.RequestsToAddress(r.Context(), req.Address, req.UserID)
	if err != nil {
		s.serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, struct {
		requestView
		Requester   *store.Requester `json:"requester"`
		History     []*store.Request `json:"history"`      // their other requests, newest first
		SameAddress []*store.Request `json:"same_address"` // other people's requests to this address
	}{out[0], who, history, nonNilRequests(same)})
}

func nonNilRequests(rs []*store.Request) []*store.Request {
	if rs == nil {
		return []*store.Request{}
	}
	return rs
}

// agentNote adds a signed paragraph to the request's internal note, so an
// agent never overwrites what an admin (or an earlier run) wrote.
func (s *server) agentNote(w http.ResponseWriter, r *http.Request, v viewer) {
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
	note := strings.TrimSpace(body.Note)
	if note == "" || len(note) > 4000 {
		writeErr(w, http.StatusUnprocessableEntity, "note must be 1 to 4000 characters")
		return
	}
	before, err := s.Store.GetRequest(r.Context(), id)
	if err != nil {
		s.respondRequest(w, nil, err)
		return
	}
	signed := fmt.Sprintf("**%s**, %s UTC:\n%s", v.key.Name, s.Now().UTC().Format("2006-01-02 15:04"), note)
	req, err := s.Store.AppendInternalNote(r.Context(), id, signed)
	if err == nil {
		s.trackRequest(r, v, "internal_note_updated", req, map[string]any{"has_note": true, "chars": len(req.InternalNote), "from_chars": len(before.InternalNote), "appended": true})
	}
	s.respondRequest(w, req, err)
}

// ---- admins manage keys (signed in; a key can't make or see keys) ----

func (s *server) adminAPIKeys(w http.ResponseWriter, r *http.Request, v viewer) {
	keys, err := s.Store.ListAPIKeys(r.Context())
	if err != nil {
		s.serverErr(w, err)
		return
	}
	if keys == nil {
		keys = []store.APIKey{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": keys})
}

func (s *server) adminCreateAPIKey(w http.ResponseWriter, r *http.Request, v viewer) {
	var body struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" || len(name) > 80 {
		writeErr(w, http.StatusUnprocessableEntity, "give the key a name (up to 80 characters), e.g. the agent that uses it")
		return
	}
	k, tok, err := s.Store.CreateAPIKey(r.Context(), name, v.user.ID)
	if err != nil {
		s.serverErr(w, err)
		return
	}
	s.trackAdmin(r, v, "api_key_created", map[string]any{"api_key_id": k.ID})
	writeJSON(w, http.StatusCreated, map[string]any{"key": k, "token": tok})
}

func (s *server) adminRevokeAPIKey(w http.ResponseWriter, r *http.Request, v viewer) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	k, err := s.Store.RevokeAPIKey(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "API key not found")
		return
	} else if err != nil {
		s.serverErr(w, err)
		return
	}
	s.trackAdmin(r, v, "api_key_revoked", map[string]any{"api_key_id": k.ID})
	writeJSON(w, http.StatusOK, k)
}
