package httpapi

import (
	"context"
	"net/http"
	"strconv"

	"github.com/hackclub/merch/internal/store"
	"github.com/hackclub/merch/internal/stripe"
	"github.com/hackclub/merch/internal/swag"
)

// Payments takes international shipping payments by card (Stripe in
// production). Without it, requests link to the HCB donation page instead.
type Payments interface {
	CreatePayment(ctx context.Context, in stripe.PaymentInput) (*stripe.Payment, error)
	GetPayment(ctx context.Context, id string) (*stripe.Payment, error)
	CancelPayment(ctx context.Context, id string) error
}

// stripeKey is the publishable key the browser's card form uses, or "" when
// card payments are off.
func (s *server) stripeKey() string {
	if s.Payments == nil {
		return ""
	}
	return s.Config.StripePublishableKey
}

// payRequest starts (or picks up) the card payment for a request's shipping
// and hands the browser its client secret; the card form on the page confirms
// it with Stripe directly. A payment that already went through is recorded
// instead of charged again.
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
	if req.Status != swag.StatusAwaitingPayment {
		writeErr(w, http.StatusConflict, "this request has nothing to pay")
		return
	}
	prev := req.StripePaymentIntent
	if prev != "" {
		p, err := s.Payments.GetPayment(r.Context(), prev)
		if err != nil {
			// Don't start a second payment when we can't tell whether the first went through.
			s.Log.Error("stripe: get payment", "request", id, "err", err)
			writeErr(w, http.StatusBadGateway, "couldn't reach the card processor; try again in a minute")
			return
		}
		switch {
		case p.Paid():
			s.syncPayment(r, req)
			writeErr(w, http.StatusConflict, "this request is already paid")
			return
		case p.Status == "processing":
			writeErr(w, http.StatusConflict, "your payment is still processing; check back in a few minutes")
			return
		case p.Open() && p.Amount == req.ShippingFeeCents:
			s.track(r, v.user, "payment_started", map[string]any{"request_id": id, "fee_cents": req.ShippingFeeCents, "country": req.Address.Country, "retry": true})
			writeJSON(w, http.StatusOK, map[string]any{"client_secret": p.ClientSecret, "amount_cents": p.Amount})
			return
		}
	}
	key := s.Config.TheseusOrderTag + "-request-" + strconv.FormatInt(id, 10) + "-pay"
	if prev != "" {
		key += "-after-" + prev // the last one was cancelled
	}
	p, err := s.Payments.CreatePayment(r.Context(), stripe.PaymentInput{
		RequestID: id, AmountCents: req.ShippingFeeCents, Email: v.user.Email, IdempotencyKey: key,
		Description: "Shipping for Hack Club swag request #" + strconv.FormatInt(id, 10),
	})
	if err != nil {
		s.Log.Error("stripe: create payment", "request", id, "err", err)
		writeErr(w, http.StatusBadGateway, "couldn't reach the card processor; try again in a minute")
		return
	}
	if err := s.Store.SetStripePayment(r.Context(), id, p.ID); err != nil {
		s.serverErr(w, err)
		return
	}
	s.track(r, v.user, "payment_started", map[string]any{"request_id": id, "fee_cents": req.ShippingFeeCents, "country": req.Address.Country, "retry": prev != ""})
	writeJSON(w, http.StatusOK, map[string]any{"client_secret": p.ClientSecret, "amount_cents": p.Amount})
}

// syncPayment checks Stripe for a request still waiting on its shipping
// payment and, once it's paid in full, moves it to review. Errors leave the
// request as it was; the next look tries again.
func (s *server) syncPayment(r *http.Request, req *store.Request) *store.Request {
	if s.Payments == nil || req.Status != swag.StatusAwaitingPayment || req.StripePaymentIntent == "" {
		return req
	}
	p, err := s.Payments.GetPayment(r.Context(), req.StripePaymentIntent)
	if err != nil {
		s.Log.Warn("stripe: get payment", "request", req.ID, "err", err)
		return req
	}
	if !p.Paid() {
		return req
	}
	if p.RequestID() != req.ID || p.AmountReceived != req.ShippingFeeCents || p.Currency != "usd" {
		s.Log.Error("stripe: payment doesn't match request", "request", req.ID, "payment", p.ID, "received", p.AmountReceived, "currency", p.Currency)
		return req
	}
	paid, err := s.Store.MarkPaidByStripe(r.Context(), req.ID, p.ID)
	if err != nil {
		s.Log.Warn("stripe: mark paid", "request", req.ID, "err", err)
		return req
	}
	s.track(r, &store.User{ID: req.UserID}, "payment_completed", map[string]any{"request_id": req.ID, "fee_cents": req.ShippingFeeCents, "country": req.Address.Country})
	s.emailRequest(r, paid, "received")
	return paid
}

func (s *server) syncPayments(r *http.Request, rs []*store.Request) {
	for i, req := range rs {
		rs[i] = s.syncPayment(r, req)
	}
}

// closePayment cancels a cancelled request's unpaid payment, so it can't be
// paid after all.
func (s *server) closePayment(r *http.Request, req *store.Request) {
	if s.Payments == nil || req.StripePaymentIntent == "" {
		return
	}
	if err := s.Payments.CancelPayment(r.Context(), req.StripePaymentIntent); err != nil {
		s.Log.Warn("stripe: cancel payment", "request", req.ID, "err", err)
	}
}
