package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/hackclub/merch/internal/store"
)

// adminChangeLines records that a dispatched request the warehouse hasn't
// mailed yet ships a different quantity of some lines: down to 0, or back up to
// what was asked for (undoing an earlier cut). mail.hackclub.com has no API to
// change an order, so the admin edits the warehouse order in its back office
// first (ships-with SKUs included); this keeps the request in step and emails
// the requester what this change did. Card-paid requests are refused: their
// postage was priced on what they ship, and this makes no refund or charge.
func (s *server) adminChangeLines(w http.ResponseWriter, r *http.Request, v viewer) {
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
	if err != nil {
		s.respondRequest(w, nil, err)
		return
	}
	switch {
	case req.Source != "app":
		writeErr(w, http.StatusConflict, "imported requests ship through the Airtable warehouse base, not from here")
		return
	case req.Status != "dispatched":
		writeErr(w, http.StatusConflict, "request is "+string(req.Status)+"; only dispatched requests can be changed here (change a pending one when you ship it)")
		return
	case req.MailedAt != nil || req.TrackingNumber != "":
		writeErr(w, http.StatusConflict, "the warehouse already mailed this request")
		return
	case req.PaidAt != nil:
		writeErr(w, http.StatusConflict, "this request's postage was paid by card; cutting it would owe a refund, which this doesn't make")
		return
	}
	quantities := map[string]int{}
	var changes []lineChange
	lowered, raised := 0, 0
	for _, l := range body.Lines {
		i := slices.IndexFunc(req.Lines, func(rl store.RequestLine) bool { return rl.SKU == l.SKU })
		if i < 0 {
			writeErr(w, http.StatusUnprocessableEntity, l.SKU+" isn't in this request")
			return
		}
		rl := req.Lines[i]
		asked := rl.Quantity
		if rl.RequestedQuantity != nil && *rl.RequestedQuantity > asked {
			asked = *rl.RequestedQuantity
		}
		if l.Quantity < 0 || l.Quantity > asked {
			writeErr(w, http.StatusUnprocessableEntity, fmt.Sprintf("%s can go from 0 up to %d (what was asked for), not %d", l.SKU, asked, l.Quantity))
			return
		}
		if l.Quantity == rl.Quantity {
			continue
		}
		if _, dup := quantities[l.SKU]; dup {
			continue
		}
		quantities[l.SKU] = l.Quantity
		changes = append(changes, lineChange{Name: lineName(rl), Asked: asked, From: rl.Quantity, To: l.Quantity})
		if l.Quantity < rl.Quantity {
			lowered++
		} else {
			raised++
		}
	}
	if len(quantities) == 0 {
		writeErr(w, http.StatusUnprocessableEntity, "nothing to change: give different quantities")
		return
	}
	left := 0
	for _, l := range req.Lines {
		if q, ok := quantities[l.SKU]; ok {
			left += q
		} else {
			left += l.Quantity
		}
	}
	if left == 0 {
		writeErr(w, http.StatusUnprocessableEntity, "nothing left to ship; cancel the warehouse order instead")
		return
	}
	req, err = s.Store.ChangeDispatchedLines(r.Context(), id, quantities, body.Note)
	if errors.Is(err, store.ErrAlreadyMailed) {
		writeErr(w, http.StatusConflict, err.Error())
		return
	} else if err != nil {
		s.respondRequest(w, nil, err)
		return
	}
	s.trackRequest(r, v, "request_changed", req, map[string]any{"lowered": lowered, "raised": raised, "has_note": body.Note != ""})
	// Each change gets its own email: the first keeps the plain "-changed" key.
	key := "changed"
	if prior, err := s.Store.ListEmails(r.Context(), store.EmailFilter{RequestID: id, Kind: "changed", Limit: 100}); err == nil && len(prior) > 0 {
		key = fmt.Sprintf("changed-%d", len(prior)+1)
	}
	s.sendRequestEmail(r, req, "changed", key, changedEmail(req, changes, body.Note, s.Config.BaseURL, s.Config.TheseusBaseURL))
	s.respondRequest(w, req, nil)
}
