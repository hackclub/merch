package httpapi

import (
	"context"
	"net/http"
	"strconv"

	"github.com/hackclub/merch/internal/store"
	"github.com/hackclub/merch/internal/stripe"
	"github.com/hackclub/merch/internal/swag"
)

// Payments takes international shipping payments (Stripe Checkout in
// production). Without it, requests link to the HCB donation page instead.
type Payments interface {
	CreateCheckout(ctx context.Context, in stripe.CheckoutInput) (*stripe.Session, error)
	GetCheckout(ctx context.Context, id string) (*stripe.Session, error)
	ExpireCheckout(ctx context.Context, id string) error
}

func payPath(id int64) string { return "/api/requests/" + strconv.FormatInt(id, 10) + "/pay" }

// payRequest sends the requester to the payment page for their request's
// shipping: the one already open, or a new one. A paid or settled request goes
// back to My requests.
func (s *server) payRequest(w http.ResponseWriter, r *http.Request, v viewer) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	req, err := s.Store.GetRequest(r.Context(), id)
	if err != nil || req.UserID != v.user.ID {
		writeErr(w, http.StatusNotFound, "request not found")
		return
	}
	if s.Payments == nil {
		writeErr(w, http.StatusNotFound, "card payments are off")
		return
	}
	prev := req.StripeSessionID
	if prev != "" {
		sess, err := s.Payments.GetCheckout(r.Context(), prev)
		if err != nil {
			s.Log.Warn("stripe: get checkout", "request", id, "err", err)
		} else if sess.Status == "open" && req.Status == swag.StatusAwaitingPayment {
			http.Redirect(w, r, sess.URL, http.StatusSeeOther)
			return
		}
		req = s.syncPayment(r, req)
	}
	if req.Status != swag.StatusAwaitingPayment {
		http.Redirect(w, r, "/requests", http.StatusSeeOther)
		return
	}
	origin := s.origin(r)
	key := s.Config.TheseusOrderTag + "-request-" + strconv.FormatInt(id, 10) + "-pay"
	if prev != "" {
		key += "-after-" + prev // a new page once the last one expired
	}
	sess, err := s.Payments.CreateCheckout(r.Context(), stripe.CheckoutInput{
		RequestID: id, AmountCents: req.ShippingFeeCents, Email: v.user.Email,
		Name:           "Shipping for Hack Club swag request #" + strconv.FormatInt(id, 10),
		SuccessURL:     origin + "/requests?paid=" + strconv.FormatInt(id, 10),
		CancelURL:      origin + "/requests",
		IdempotencyKey: key,
	})
	if err != nil {
		s.Log.Error("stripe: create checkout", "request", id, "err", err)
		writeErr(w, http.StatusBadGateway, "couldn't open the payment page; try again in a minute")
		return
	}
	if err := s.Store.SetStripeSession(r.Context(), id, sess.ID); err != nil {
		s.serverErr(w, err)
		return
	}
	s.track(r, v.user, "payment_started", map[string]any{"request_id": id, "fee_cents": req.ShippingFeeCents, "country": req.Address.Country})
	http.Redirect(w, r, sess.URL, http.StatusSeeOther)
}

// syncPayment checks Stripe for a request still waiting on its shipping
// payment and, once it's paid in full, moves it to review. Errors leave the
// request as it was; the next look tries again.
func (s *server) syncPayment(r *http.Request, req *store.Request) *store.Request {
	if s.Payments == nil || req.Status != swag.StatusAwaitingPayment || req.StripeSessionID == "" {
		return req
	}
	sess, err := s.Payments.GetCheckout(r.Context(), req.StripeSessionID)
	if err != nil {
		s.Log.Warn("stripe: get checkout", "request", req.ID, "err", err)
		return req
	}
	if !sess.Paid() {
		return req
	}
	if sess.RequestID() != req.ID || sess.AmountTotal != req.ShippingFeeCents || sess.Currency != "usd" {
		s.Log.Error("stripe: payment doesn't match request", "request", req.ID, "session", sess.ID, "amount", sess.AmountTotal, "currency", sess.Currency)
		return req
	}
	paid, err := s.Store.MarkPaidByStripe(r.Context(), req.ID, sess.ID, sess.PaymentIntent)
	if err != nil {
		s.Log.Warn("stripe: mark paid", "request", req.ID, "err", err)
		return req
	}
	s.track(r, &store.User{ID: req.UserID}, "payment_completed", map[string]any{"request_id": req.ID, "fee_cents": req.ShippingFeeCents, "country": req.Address.Country})
	return paid
}

func (s *server) syncPayments(r *http.Request, rs []*store.Request) {
	for i, req := range rs {
		rs[i] = s.syncPayment(r, req)
	}
}

// closePayment expires a cancelled request's open payment page, so it can't
// be paid after all.
func (s *server) closePayment(r *http.Request, req *store.Request) {
	if s.Payments == nil || req.StripeSessionID == "" {
		return
	}
	if err := s.Payments.ExpireCheckout(r.Context(), req.StripeSessionID); err != nil {
		s.Log.Warn("stripe: expire checkout", "request", req.ID, "err", err)
	}
}
