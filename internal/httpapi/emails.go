package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/hackclub/merch/internal/loops"
	"github.com/hackclub/merch/internal/store"
	"github.com/hackclub/merch/internal/swag"
)

// Mailer sends transactional emails (Loops in production). Nil sends none.
type Mailer interface {
	Send(ctx context.Context, e loops.Email) error
}

// emailRequest tells a requester what happened to their request: kind is
// received, dispatched or rejected. Each kind goes out once per request (the
// idempotency key), and a failure is logged without undoing the action. Every
// email is saved word for word in sent_emails (logEmail), even with no mailer
// configured, so audits can read what people were told.
func (s *server) emailRequest(r *http.Request, req *store.Request, kind string) {
	if req.UserEmail == "" {
		return
	}
	var e loops.Email
	switch kind {
	case "received":
		e = receivedEmail(req, s.Config.BaseURL)
	case "dispatched":
		e = shippedEmail(req, s.Config.BaseURL)
	case "rejected":
		e = rejectedEmail(req, s.Config.BaseURL)
	}
	e.IdempotencyKey = s.Config.TheseusOrderTag + "-request-" + strconv.FormatInt(req.ID, 10) + "-" + kind
	if s.Mailer == nil {
		s.logEmail(r, req, kind, e, "unsent", "")
		return
	}
	u := &store.User{ID: req.UserID}
	if err := s.Mailer.Send(r.Context(), e); err != nil {
		s.Log.Error("email not sent", "kind", kind, "request", req.ID, "err", err)
		id := s.logEmail(r, req, kind, e, "failed", err.Error())
		s.track(r, u, "email_failed", map[string]any{"kind": kind, "request_id": req.ID, "email_id": id, "error": err.Error()})
		return
	}
	id := s.logEmail(r, req, kind, e, "sent", "")
	s.track(r, u, "email_sent", map[string]any{"kind": kind, "request_id": req.ID, "email_id": id})
}

// logEmail saves an email in sent_emails and returns its id (0 if it couldn't).
func (s *server) logEmail(r *http.Request, req *store.Request, kind string, e loops.Email, status, errText string) int64 {
	row, err := s.Store.LogEmail(r.Context(), store.SentEmail{
		Kind: kind, RequestID: &req.ID, UserID: &req.UserID, To: e.To, Subject: e.Subject, Preview: e.Preview,
		Body: e.Body, IdempotencyKey: e.IdempotencyKey, Status: status, Error: errText, Release: s.release,
	})
	if err != nil {
		s.Log.Warn("email not logged", "kind", kind, "request", req.ID, "err", err)
		return 0
	}
	return row.ID
}

// adminEmails lists logged emails, newest first: ?request_id=, ?kind=,
// ?status= (sent, failed, unsent), ?before=<id> to page, ?limit= (default 100, max 500).
func (s *server) adminEmails(w http.ResponseWriter, r *http.Request, v viewer) {
	q := r.URL.Query()
	f := store.EmailFilter{Kind: q.Get("kind"), Status: q.Get("status"), Limit: 100}
	for name, dst := range map[string]*int64{"request_id": &f.RequestID, "before": &f.Before} {
		if raw := q.Get(name); raw != "" {
			n, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				writeErr(w, http.StatusBadRequest, name+" must be a number")
				return
			}
			*dst = n
		}
	}
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			writeErr(w, http.StatusBadRequest, "limit must be a positive number")
			return
		}
		f.Limit = min(n, 500)
	}
	emails, err := s.Store.ListEmails(r.Context(), f)
	if err != nil {
		s.serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"emails": emails})
}

func receivedEmail(req *store.Request, baseURL string) loops.Email {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\nThanks for your request! Here's what you asked for:\n\n%s\n\nIt's going to %s.", greeting(req), lineList(req.Lines), place(req.Address))
	if swag.IsInternational(req.Address.Country) {
		if req.ShippingFeeCents > 0 {
			fmt.Fprintf(&b, " You paid %s for postage.", dollars(req.ShippingFeeCents))
		}
		b.WriteString(" You're responsible for any customs fees or import taxes your country charges to receive a package from the USA.")
	}
	b.WriteString("\n\nSomeone at Hack Club reads every request. We'll email you again when it ships, or if we can't send it.")
	fmt.Fprintf(&b, "\n\nSee your requests any time at %s/requests.\n\nHack Club", baseURL)
	return loops.Email{
		To:      req.UserEmail,
		Subject: "We got your Hack Club swag request",
		Preview: fmt.Sprintf("Request #%d: we'll email you when it ships.", req.ID),
		Body:    b.String(),
	}
}

func shippedEmail(req *store.Request, baseURL string) loops.Email {
	var b strings.Builder
	var shipped []store.RequestLine
	var changed []string
	for _, l := range req.Lines {
		if l.Quantity > 0 {
			shipped = append(shipped, l)
		}
		switch {
		case l.RequestedQuantity == nil:
		case l.Quantity == 0:
			changed = append(changed, fmt.Sprintf("- %s: you asked for %d, we couldn't send it", lineName(l), *l.RequestedQuantity))
		default:
			changed = append(changed, fmt.Sprintf("- %s: you asked for %d, we sent %d", lineName(l), *l.RequestedQuantity, l.Quantity))
		}
	}
	fmt.Fprintf(&b, "%s\n\nGood news: we approved request #%d and sent it to our warehouse. It usually leaves within a business day or two.\n\nWhat's in the box:\n\n%s",
		greeting(req), req.ID, lineList(shipped))
	if len(changed) > 0 {
		fmt.Fprintf(&b, "\n\nWe changed your request before it shipped:\n\n%s", strings.Join(changed, "\n"))
	}
	if req.RefundedCents > 0 {
		fmt.Fprintf(&b, "\n\nSince we sent less, postage costs less: we refunded %s of the %s you paid to your card. It can take 5–10 business days to show up.",
			dollars(req.RefundedCents), dollars(req.ShippingFeeCents))
	}
	if note := strings.TrimSpace(req.AdminNote); note != "" {
		fmt.Fprintf(&b, "\n\nA note from Hack Club:\n\n%s", note)
	}
	fmt.Fprintf(&b, "\n\nIt's going to %s. Once it's mailed, its tracking number shows up at %s/requests.\n\nHack Club", place(req.Address), baseURL)
	return loops.Email{
		To:      req.UserEmail,
		Subject: "Your Hack Club swag is on its way",
		Preview: fmt.Sprintf("We approved request #%d and sent it to our warehouse.", req.ID),
		Body:    b.String(),
	}
}

func rejectedEmail(req *store.Request, baseURL string) loops.Email {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\nSorry, we can't send request #%d:\n\n%s", greeting(req), req.ID, lineList(req.Lines))
	if note := strings.TrimSpace(req.AdminNote); note != "" {
		fmt.Fprintf(&b, "\n\nWhy: %s", note)
	}
	switch {
	case req.RefundedAt != nil:
		fmt.Fprintf(&b, "\n\nWe refunded the %s you paid for postage to your card. It can take 5–10 business days to show up.", dollars(req.ShippingFeeCents))
	}
	fmt.Fprintf(&b, "\n\nSee your requests at %s/requests.\n\nHack Club", baseURL)
	return loops.Email{
		To:      req.UserEmail,
		Subject: "We can't send your Hack Club swag request",
		Preview: fmt.Sprintf("Request #%d won't ship.", req.ID),
		Body:    b.String(),
	}
}

func greeting(req *store.Request) string {
	if n := strings.TrimSpace(req.Address.FirstName); n != "" {
		return "Hi " + n + ","
	}
	return "Hi,"
}

// lineList is one "- 2 × Sticker" line per request line.
func lineList(lines []store.RequestLine) string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = fmt.Sprintf("- %d × %s", l.Quantity, lineName(l))
	}
	return strings.Join(out, "\n")
}

func lineName(l store.RequestLine) string {
	if l.Variant != "" {
		return l.Name + " (" + l.Variant + ")"
	}
	return l.Name
}

// place is the city, state and country (never the street).
func place(a swag.Address) string {
	var parts []string
	for _, p := range []string{a.City, a.State, a.Country} {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, ", ")
}

func dollars(cents int) string { return fmt.Sprintf("$%d.%02d", cents/100, cents%100) }
