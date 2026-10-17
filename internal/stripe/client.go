// Package stripe takes international shipping payments by card. The card form
// is Stripe's Payment Element inside our page, so card details go straight to
// Stripe and never touch our server.
// Docs: https://docs.stripe.com/api/payment_intents
package stripe

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	baseURL string
	key     string // secret key (sk_test_… in the sandbox, sk_live_… for real money)
	http    *http.Client
}

func New(baseURL, key string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), key: key, http: hc}
}

// PaymentInput is one request's shipping payment, by card.
type PaymentInput struct {
	RequestID      int64
	AmountCents    int    // USD
	Description    string // on the card statement's receipt and in the dashboard
	Email          string // where Stripe sends the receipt
	IdempotencyKey string // so a retried call doesn't make a second payment
}

// Payment is a PaymentIntent: one attempt to collect an amount.
type Payment struct {
	ID             string            `json:"id"`
	ClientSecret   string            `json:"client_secret"` // the browser confirms the card with it
	Status         string            `json:"status"`        // requires_payment_method, requires_action, processing, succeeded, canceled…
	Amount         int               `json:"amount"`
	AmountReceived int               `json:"amount_received"`
	Currency       string            `json:"currency"`
	Metadata       map[string]string `json:"metadata"`
}

// Paid reports whether the money has been collected.
func (p *Payment) Paid() bool { return p.Status == "succeeded" }

// Open reports whether the payment can still be paid (or retried with another card).
func (p *Payment) Open() bool {
	switch p.Status {
	case "requires_payment_method", "requires_confirmation", "requires_action":
		return true
	}
	return false
}

// RequestID is the swag request this payment is for (0 if none).
func (p *Payment) RequestID() int64 {
	id, _ := strconv.ParseInt(p.Metadata["request_id"], 10, 64)
	return id
}

// CreatePayment starts a card payment the browser then confirms with the
// Payment Element (https://docs.stripe.com/payments/accept-a-payment-deferred).
func (c *Client) CreatePayment(ctx context.Context, in PaymentInput) (*Payment, error) {
	f := url.Values{
		"amount":                          {strconv.Itoa(in.AmountCents)},
		"currency":                        {"usd"},
		"allowed_payment_method_types[0]": {"card"},
		"description":                     {in.Description},
		"metadata[request_id]":            {strconv.FormatInt(in.RequestID, 10)},
	}
	if in.Email != "" {
		f.Set("receipt_email", in.Email)
	}
	var p Payment
	return &p, c.call(ctx, "POST", "/v1/payment_intents", f, in.IdempotencyKey, &p)
}

func (c *Client) GetPayment(ctx context.Context, id string) (*Payment, error) {
	if !validID.MatchString(id) {
		return nil, fmt.Errorf("stripe: bad payment id %q", id)
	}
	var p Payment
	return &p, c.call(ctx, "GET", "/v1/payment_intents/"+id, nil, "", &p)
}

// CancelPayment stops an unpaid payment so it can't be paid any more.
func (c *Client) CancelPayment(ctx context.Context, id string) error {
	if !validID.MatchString(id) {
		return fmt.Errorf("stripe: bad payment id %q", id)
	}
	return c.call(ctx, "POST", "/v1/payment_intents/"+id+"/cancel", url.Values{}, "", nil)
}

// Refund gives a paid payment's money back to the card: amountCents of it, or
// all of it for 0.
func (c *Client) Refund(ctx context.Context, paymentID string, amountCents int, idempotencyKey string) error {
	if !validID.MatchString(paymentID) {
		return fmt.Errorf("stripe: bad payment id %q", paymentID)
	}
	f := url.Values{"payment_intent": {paymentID}}
	if amountCents > 0 {
		f.Set("amount", strconv.Itoa(amountCents))
	}
	return c.call(ctx, "POST", "/v1/refunds", f, idempotencyKey, nil)
}

// Refunded is how much of a payment has gone back to the card, or is on its
// way (refunds that failed or were cancelled don't count).
func (c *Client) Refunded(ctx context.Context, paymentID string) (int, error) {
	if !validID.MatchString(paymentID) {
		return 0, fmt.Errorf("stripe: bad payment id %q", paymentID)
	}
	var list struct {
		Data []struct {
			Amount int    `json:"amount"`
			Status string `json:"status"`
		} `json:"data"`
	}
	q := url.Values{"payment_intent": {paymentID}, "limit": {"100"}}
	if err := c.call(ctx, "GET", "/v1/refunds?"+q.Encode(), nil, "", &list); err != nil {
		return 0, err
	}
	cents := 0
	for _, r := range list.Data {
		if r.Status != "failed" && r.Status != "canceled" {
			cents += r.Amount
		}
	}
	return cents, nil
}

// ApprovalError means Stripe is holding the action for someone to approve in
// the dashboard (the account's approval rules). Once approved, Stripe carries
// it out by itself.
type ApprovalError struct {
	ID      string // apreq_…
	Message string
}

func (e *ApprovalError) Error() string {
	return "stripe: needs approval in the Stripe dashboard (" + e.ID + "): " + e.Message
}

var (
	validID    = regexp.MustCompile(`^pi_[A-Za-z0-9_]+$`)
	approvalID = regexp.MustCompile(`apreq_[A-Za-z0-9]+`)
)

func (c *Client) call(ctx context.Context, method, path string, form url.Values, idempotencyKey string, out any) error {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("stripe: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		json.Unmarshal(data, &e)
		if id := approvalID.FindString(e.Error.Message); resp.StatusCode == http.StatusForbidden && id != "" {
			return &ApprovalError{ID: id, Message: e.Error.Message}
		}
		return fmt.Errorf("stripe: %d: %s", resp.StatusCode, e.Error.Message)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(data, out)
}
