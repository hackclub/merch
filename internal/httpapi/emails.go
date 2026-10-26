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
// received, dispatched or rejected (changed goes through sendRequestEmail). Each kind goes out once per request (the
// idempotency key), and a failure is logged without undoing the action. Every
// email is saved word for word in sent_emails (logEmail), even with no mailer
// configured, so audits can read what people were told. waiting names the
// dispatched lines still on their way to the warehouse (backordered).
func (s *server) emailRequest(r *http.Request, req *store.Request, kind string, waiting ...string) {
	if req.UserEmail == "" {
		return
	}
	var e loops.Email
	switch kind {
	case "received":
		e = receivedEmail(req, s.Config.BaseURL)
	case "dispatched":
		e = shippedEmail(req, s.Config.BaseURL, s.Config.TheseusBaseURL, waiting)
	case "rejected":
		e = rejectedEmail(req, s.Config.BaseURL)
	}
	s.sendRequestEmail(r, req, kind, kind, e)
}

// sendRequestEmail sends and logs one request email; key makes its idempotency
// key unique (the kind, for emails that go out once per request).
func (s *server) sendRequestEmail(r *http.Request, req *store.Request, kind, key string, e loops.Email) {
	if req.UserEmail == "" {
		return
	}
	e.IdempotencyKey = s.Config.TheseusOrderTag + "-request-" + strconv.FormatInt(req.ID, 10) + "-" + key
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

// shippedEmail is the approval email. mail.hackclub.com (Theseus) sends its own
// email with the tracking number once the warehouse mails the package, so this
// one says that's coming and links the package's public page there. waiting
// names the lines still on their way to the warehouse, which hold the package.
func shippedEmail(req *store.Request, baseURL, mailURL string, waiting []string) loops.Email {
	var b strings.Builder
	var shipped []store.RequestLine
	var changed []string
	for _, l := range req.Lines {
		if l.Quantity > 0 {
			shipped = append(shipped, l)
		}
		switch {
		case l.RequestedQuantity == nil:
		case *l.RequestedQuantity == 0:
			changed = append(changed, fmt.Sprintf("- %s: we added %d", lineName(l), l.Quantity))
		case l.Quantity == 0:
			changed = append(changed, fmt.Sprintf("- %s: you asked for %d, we left it out of this package", lineName(l), *l.RequestedQuantity))
		default:
			changed = append(changed, fmt.Sprintf("- %s: you asked for %d, we sent %d", lineName(l), *l.RequestedQuantity, l.Quantity))
		}
	}
	leaves := "It usually leaves within a business day or two."
	switch len(waiting) {
	case 0:
	case 1:
		leaves = fmt.Sprintf("The %s in it is still on its way to the warehouse, so your package ships as soon as it arrives.", waiting[0])
	default:
		leaves = fmt.Sprintf("The %s in it are still on their way to the warehouse, so your package ships as soon as they arrive.", andList(waiting))
	}
	fmt.Fprintf(&b, "%s\n\nGood news: we approved request #%d and sent it to our warehouse. %s\n\nWhat's in the box:\n\n%s",
		greeting(req), req.ID, leaves, lineList(shipped))
	note := strings.TrimSpace(req.AdminNote)
	if len(changed) > 0 {
		why := ""
		if note != "" {
			why = " (the note below says why)"
		}
		fmt.Fprintf(&b, "\n\nWe changed your request before it shipped%s:\n\n%s", why, strings.Join(changed, "\n"))
	}
	if req.RefundedCents > 0 {
		fmt.Fprintf(&b, "\n\nSince we sent less, postage costs less: we refunded %s of the %s you paid to your card. It can take 5–10 business days to show up.",
			dollars(req.RefundedCents), dollars(req.ShippingFeeCents))
	}
	if note != "" {
		fmt.Fprintf(&b, "\n\nA note from Hack Club:\n\n%s", note)
	}
	fmt.Fprintf(&b, "\n\nIt's going to %s.\n\nTracking: when it's mailed, you'll get an email from mail.hackclub.com with the tracking number.", place(req.Address))
	if req.TheseusOrderID != "" {
		fmt.Fprintf(&b, " Until then, you can follow it at %s/packages/%s.", mailURL, req.TheseusOrderID)
	}
	fmt.Fprintf(&b, " See all your requests at %s/requests.\n\nHack Club", baseURL)
	return loops.Email{
		To:      req.UserEmail,
		Subject: "Your Hack Club swag is on its way",
		Preview: fmt.Sprintf("We approved request #%d and sent it to our warehouse.", req.ID),
		Body:    b.String(),
	}
}

// lineChange is one line adminChangeLines changed: what was asked for, and
// what it shipped before and after.
type lineChange struct {
	Name            string
	Asked, From, To int
}

// changedEmail tells a requester what one change to their dispatched request
// did before the warehouse mailed it (adminChangeLines): only the lines it
// changed, the note saying why, and what's in the box now.
func changedEmail(req *store.Request, changes []lineChange, note, baseURL, mailURL string) loops.Email {
	var b strings.Builder
	var shipped []store.RequestLine
	for _, l := range req.Lines {
		if l.Quantity > 0 {
			shipped = append(shipped, l)
		}
	}
	lines := make([]string, len(changes))
	for i, c := range changes {
		switch {
		case c.To > c.From:
			lines[i] = fmt.Sprintf("- %s: we're sending %d after all, not %d", c.Name, c.To, c.From)
		case c.To == 0:
			lines[i] = fmt.Sprintf("- %s: you asked for %d, we're not sending any this time", c.Name, c.Asked)
		default:
			lines[i] = fmt.Sprintf("- %s: you asked for %d, we're sending %d", c.Name, c.Asked, c.To)
		}
	}
	fmt.Fprintf(&b, "%s\n\nWe changed request #%d before our warehouse mailed it:\n\n%s", greeting(req), req.ID, strings.Join(lines, "\n"))
	if note = strings.TrimSpace(note); note != "" {
		fmt.Fprintf(&b, "\n\nA note from Hack Club:\n\n%s", note)
	}
	fmt.Fprintf(&b, "\n\nThe rest of your request hasn't changed. What's in the box now:\n\n%s\n\nIt's going to %s.", lineList(shipped), place(req.Address))
	b.WriteString("\n\nTracking: when it's mailed, you'll get an email from mail.hackclub.com with the tracking number.")
	if req.TheseusOrderID != "" {
		fmt.Fprintf(&b, " Until then, you can follow it at %s/packages/%s.", mailURL, req.TheseusOrderID)
	}
	fmt.Fprintf(&b, " See all your requests at %s/requests.\n\nHack Club", baseURL)
	return loops.Email{
		To:      req.UserEmail,
		Subject: fmt.Sprintf("We changed your Hack Club swag request #%d", req.ID),
		Preview: fmt.Sprintf("We changed request #%d before it was mailed.", req.ID),
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

// andList joins names as "a", "a and b", "a, b and c".
func andList(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
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
