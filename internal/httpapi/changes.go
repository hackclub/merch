package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/hackclub/merch/internal/store"
)

// adminReduceLines records that a dispatched request the warehouse hasn't
// mailed yet ships less than it did (quantities by SKU, only ever lower), for
// when the warehouse runs short of something already sent to it. mail.hackclub.com
// has no API to change an order, so the admin edits the warehouse order in its
// back office first (ships-with SKUs included); this keeps the request in step
// and emails the requester what changed. Card-paid requests are refused: their
// postage was priced on what they ship, and this makes no refund.
func (s *server) adminReduceLines(w http.ResponseWriter, r *http.Request, v viewer) {
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
	for _, l := range body.Lines {
		i := slices.IndexFunc(req.Lines, func(rl store.RequestLine) bool { return rl.SKU == l.SKU })
		switch {
		case i < 0:
			writeErr(w, http.StatusUnprocessableEntity, l.SKU+" isn't in this request")
			return
		case l.Quantity < 0 || l.Quantity > req.Lines[i].Quantity:
			writeErr(w, http.StatusUnprocessableEntity, fmt.Sprintf("%s can go from %d down to 0, not to %d", l.SKU, req.Lines[i].Quantity, l.Quantity))
			return
		case l.Quantity < req.Lines[i].Quantity:
			quantities[l.SKU] = l.Quantity
		}
	}
	if len(quantities) == 0 {
		writeErr(w, http.StatusUnprocessableEntity, "nothing to change: give lower quantities")
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
	req, err = s.Store.ReduceDispatchedLines(r.Context(), id, quantities, body.Note)
	if errors.Is(err, store.ErrAlreadyMailed) {
		writeErr(w, http.StatusConflict, err.Error())
		return
	} else if err != nil {
		s.respondRequest(w, nil, err)
		return
	}
	s.trackRequest(r, v, "request_changed", req, map[string]any{"lowered": len(quantities), "has_note": body.Note != ""})
	s.emailRequest(r, req, "changed")
	s.respondRequest(w, req, nil)
}
