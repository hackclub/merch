package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/hackclub/merch/internal/store"
	"github.com/hackclub/merch/internal/stripe"
	"github.com/hackclub/merch/internal/swag"
)

// Payments takes international shipping payments by card (Stripe).
type Payments interface {
	CreatePayment(ctx context.Context, in stripe.PaymentInput) (*stripe.Payment, error)
	GetPayment(ctx context.Context, id string) (*stripe.Payment, error)
	CancelPayment(ctx context.Context, id string) error
	Refund(ctx context.Context, id string, amountCents int, idempotencyKey string) error // 0 refunds all of it
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
	if req.Status != swag.StatusAwaitingPayment || req.StripePaymentIntent == "" {
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

// settlePayment squares a request with its payer before it's rejected or
// cancelled (by "admin" or "requester"): an unpaid payment is closed, and a
// card payment that went through is refunded. An error means Stripe didn't
// refund it, so the request should stay as it is.
func (s *server) settlePayment(r *http.Request, req *store.Request, by string) (_ *store.Request, refunded bool, err error) {
	if req.Status == swag.StatusAwaitingPayment {
		if req = s.syncPayment(r, req); req.Status == swag.StatusAwaitingPayment {
			s.closePayment(r, req)
			return req, false, nil
		}
	}
	if req.PaidAt == nil || req.StripePaymentIntent == "" || req.RefundedAt != nil {
		return req, false, nil
	}
	key := s.Config.TheseusOrderTag + "-request-" + strconv.FormatInt(req.ID, 10) + "-refund"
	if err := s.Payments.Refund(r.Context(), req.StripePaymentIntent, 0, key); err != nil {
		s.Log.Error("stripe: refund", "request", req.ID, "err", err)
		return req, false, err
	}
	s.track(r, &store.User{ID: req.UserID}, "payment_refunded", map[string]any{"request_id": req.ID, "fee_cents": req.ShippingFeeCents, "refund_cents": req.ShippingFeeCents, "country": req.Address.Country, "by": by})
	return req, true, nil
}

// refundUnusedPostage gives back postage a card-paid request no longer needs
// after an admin shipped less of it: what it paid, minus the fee for what
// actually shipped. It returns a warning for the admin if that didn't work.
func (s *server) refundUnusedPostage(r *http.Request, req *store.Request) (*store.Request, string) {
	if req.PaidAt == nil || req.StripePaymentIntent == "" || req.ShippingFeeCents == 0 || req.RefundedCents > 0 {
		return req, ""
	}
	ctx := r.Context()
	var lines []swag.Line
	for _, l := range req.Lines {
		if l.Quantity > 0 {
			lines = append(lines, swag.Line{ItemID: l.ItemID, SKU: l.SKU, Quantity: l.Quantity})
		}
	}
	st, err := s.Store.GetSettings(ctx)
	if err != nil {
		return req, "shipped, but couldn't check whether postage is now cheaper: " + err.Error()
	}
	items, err := s.Store.ListItems(ctx, false)
	if err != nil {
		return req, "shipped, but couldn't check whether postage is now cheaper: " + err.Error()
	}
	cat := map[int64]swag.Item{}
	for _, it := range items {
		cat[it.ID] = it
	}
	_, est, err := s.quote(ctx, st, req.Address.Country, lines, cat)
	if err != nil {
		return req, "shipped, but couldn't estimate postage for what shipped, so nothing was refunded: " + err.Error()
	}
	diff := req.ShippingFeeCents - est.FeeCents
	if diff <= 0 {
		return req, ""
	}
	key := s.Config.TheseusOrderTag + "-request-" + strconv.FormatInt(req.ID, 10) + "-refund-postage"
	if err := s.Payments.Refund(ctx, req.StripePaymentIntent, diff, key); err != nil {
		s.Log.Error("stripe: refund postage difference", "request", req.ID, "err", err)
		return req, fmt.Sprintf("shipped, but Stripe didn't refund the %s postage difference (%v); refund it in the Stripe dashboard", dollars(diff), err)
	}
	s.track(r, &store.User{ID: req.UserID}, "payment_refunded", map[string]any{"request_id": req.ID, "fee_cents": req.ShippingFeeCents, "refund_cents": diff, "country": req.Address.Country, "by": "admin", "partial": true})
	upd, err := s.Store.RecordRefund(ctx, req.ID, diff)
	if err != nil {
		s.Log.Error("record refund", "request", req.ID, "err", err)
		return req, fmt.Sprintf("refunded %s, but couldn't record it: %v", dollars(diff), err)
	}
	return upd, ""
}

// closePayment cancels a cancelled request's unpaid payment, so it can't be
// paid after all.
func (s *server) closePayment(r *http.Request, req *store.Request) {
	if req.StripePaymentIntent == "" {
		return
	}
	if err := s.Payments.CancelPayment(r.Context(), req.StripePaymentIntent); err != nil {
		s.Log.Warn("stripe: cancel payment", "request", req.ID, "err", err)
	}
}
