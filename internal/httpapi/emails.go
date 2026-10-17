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
// idempotency key), and a failure is logged without undoing the action.
func (s *server) emailRequest(r *http.Request, req *store.Request, kind string) {
	if s.Mailer == nil || req.UserEmail == "" {
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
	u := &store.User{ID: req.UserID}
	if err := s.Mailer.Send(r.Context(), e); err != nil {
		s.Log.Error("email not sent", "kind", kind, "request", req.ID, "err", err)
		s.track(r, u, "email_failed", map[string]any{"kind": kind, "request_id": req.ID, "error": err.Error()})
		return
	}
	s.track(r, u, "email_sent", map[string]any{"kind": kind, "request_id": req.ID})
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
	fmt.Fprintf(&b, "%s\n\nGood news: we approved request #%d and sent it to our warehouse. It usually leaves within a business day or two.\n\nWhat's in the box:\n\n%s",
		greeting(req), req.ID, lineList(req.Lines))
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
