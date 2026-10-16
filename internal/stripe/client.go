// Package stripe takes international shipping payments through Stripe Checkout
// (hosted payment pages), so card details never touch our server.
// Docs: https://docs.stripe.com/api/checkout/sessions
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

// CheckoutInput is one request's shipping payment.
type CheckoutInput struct {
	RequestID      int64
	AmountCents    int    // USD
	Name           string // what the payment page says it's for
	Email          string // prefills the receipt email
	SuccessURL     string
	CancelURL      string
	IdempotencyKey string // so a retried call doesn't open a second session
}

// Session is a Checkout Session: a hosted payment page and its outcome.
type Session struct {
	ID                string            `json:"id"`
	URL               string            `json:"url"`            // the payment page, while open
	Status            string            `json:"status"`         // open, complete, expired
	PaymentStatus     string            `json:"payment_status"` // paid, unpaid, no_payment_required
	AmountTotal       int               `json:"amount_total"`
	Currency          string            `json:"currency"`
	PaymentIntent     string            `json:"payment_intent"`
	ClientReferenceID string            `json:"client_reference_id"`
	Metadata          map[string]string `json:"metadata"`
}

// Paid reports whether the money has been collected.
func (s *Session) Paid() bool { return s.PaymentStatus == "paid" }

// RequestID is the swag request this session pays for (0 if none).
func (s *Session) RequestID() int64 {
	id, _ := strconv.ParseInt(s.Metadata["request_id"], 10, 64)
	return id
}

func (c *Client) CreateCheckout(ctx context.Context, in CheckoutInput) (*Session, error) {
	id := strconv.FormatInt(in.RequestID, 10)
	f := url.Values{
		"mode":                                          {"payment"},
		"line_items[0][quantity]":                       {"1"},
		"line_items[0][price_data][currency]":           {"usd"},
		"line_items[0][price_data][unit_amount]":        {strconv.Itoa(in.AmountCents)},
		"line_items[0][price_data][product_data][name]": {in.Name},
		"client_reference_id":                           {id},
		"metadata[request_id]":                          {id},
		"payment_intent_data[metadata][request_id]":     {id},
		"success_url":                                   {in.SuccessURL},
		"cancel_url":                                    {in.CancelURL},
	}
	if in.Email != "" {
		f.Set("customer_email", in.Email)
	}
	var s Session
	return &s, c.call(ctx, "POST", "/v1/checkout/sessions", f, in.IdempotencyKey, &s)
}

func (c *Client) GetCheckout(ctx context.Context, id string) (*Session, error) {
	if !validID.MatchString(id) {
		return nil, fmt.Errorf("stripe: bad session id %q", id)
	}
	var s Session
	return &s, c.call(ctx, "GET", "/v1/checkout/sessions/"+id, nil, "", &s)
}

// ExpireCheckout closes an open payment page so it can't be paid any more.
func (c *Client) ExpireCheckout(ctx context.Context, id string) error {
	if !validID.MatchString(id) {
		return fmt.Errorf("stripe: bad session id %q", id)
	}
	return c.call(ctx, "POST", "/v1/checkout/sessions/"+id+"/expire", url.Values{}, "", nil)
}

var validID = regexp.MustCompile(`^cs_[A-Za-z0-9_]+$`)

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
		return fmt.Errorf("stripe: %d: %s", resp.StatusCode, e.Error.Message)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(data, out)
}
