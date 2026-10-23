// Package theseus is a client for the mail.hackclub.com ("Theseus") warehouse
// API: SKU inventory and warehouse orders fulfilled by the Hack Club warehouse.
// Source of truth for the API: github.com/hackclub/theseus
// (app/controllers/api/v1/warehouse_*_controller.rb).
package theseus

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client

	// BillingProfileID is the HCB billing profile (bp!…) every order is
	// charged to. Empty falls back to the API key's default profile.
	BillingProfileID string
}

func New(baseURL, apiKey string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{baseURL: baseURL, apiKey: apiKey, http: hc}
}

type SKU struct {
	SKU         string `json:"sku"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Enabled     bool   `json:"enabled"`
	InStock     int    `json:"in_stock"`
	Inbound     *int   `json:"inbound"`
	UnitCost    string `json:"unit_cost"`
	// What it declares for customs abroad, where it has them.
	HSCode             string `json:"hs_code"`
	CountryOfOrigin    string `json:"country_of_origin"`
	CustomsDescription string `json:"customs_description"`
}

type Address struct {
	FirstName  string `json:"first_name"`
	LastName   string `json:"last_name"`
	Line1      string `json:"line_1"`
	Line2      string `json:"line_2,omitempty"`
	City       string `json:"city"`
	State      string `json:"state"`
	PostalCode string `json:"postal_code"`
	Country    string `json:"country"`
	Phone      string `json:"phone_number,omitempty"`
}

type Content struct {
	SKU      string `json:"sku"`
	Quantity int    `json:"quantity"`
}

type OrderInput struct {
	RecipientEmail string
	Title          string
	IdempotencyKey string
	Tags           []string
	Metadata       map[string]any
	Address        Address
	Contents       []Content
}

type Order struct {
	ID             string     `json:"id"`
	Status         string     `json:"status"`
	Tags           []string   `json:"tags"`
	TrackingNumber string     `json:"tracking_number"`
	Carrier        string     `json:"carrier"`
	Service        string     `json:"service"`
	MailedAt       *time.Time `json:"mailed_at"`
	ContentsCost   any        `json:"contents_cost"` // dollars, as a string or a number
	LaborCost      any        `json:"labor_cost"`
	PostageCost    any        `json:"postage_cost"` // set once it's mailed
}

// Costs is what the warehouse charged for an order, in cents; nil where it
// hasn't said yet (postage comes when it's mailed).
type Costs struct {
	ContentsCents *int `json:"contents_cents"`
	LaborCents    *int `json:"labor_cents"`
	PostageCents  *int `json:"postage_cents"`
}

func (o *Order) Costs() Costs {
	return Costs{cents(o.ContentsCost), cents(o.LaborCost), cents(o.PostageCost)}
}

func cents(v any) *int {
	var d float64
	switch x := v.(type) {
	case float64:
		d = x
	case string:
		f, err := strconv.ParseFloat(x, 64)
		if err != nil {
			return nil
		}
		d = f
	default:
		return nil
	}
	c := int(math.Round(d * 100))
	return &c
}

// ListSKUs returns enabled, in-inventory SKUs.
func (c *Client) ListSKUs(ctx context.Context) ([]SKU, error) {
	var out struct {
		SKUs []SKU `json:"skus"`
	}
	err := c.do(ctx, http.MethodGet, "/api/v1/warehouse/skus", nil, &out)
	return out.SKUs, err
}

// CreateOrder creates and dispatches a warehouse order. Theseus replays the
// existing order for a repeated IdempotencyKey, so retries are safe.
func (c *Client) CreateOrder(ctx context.Context, in OrderInput) (*Order, error) {
	body := map[string]any{
		"warehouse_order": map[string]any{
			"recipient_email":   in.RecipientEmail,
			"user_facing_title": in.Title,
			"idempotency_key":   in.IdempotencyKey,
			"tags":              in.Tags,
			"metadata":          in.Metadata,
		},
		"address":  in.Address,
		"contents": in.Contents,
	}
	if c.BillingProfileID != "" {
		body["billing_profile_id"] = c.BillingProfileID
	}
	var out orderResponse
	if err := c.do(ctx, http.MethodPost, "/api/v1/warehouse_orders", body, &out); err != nil {
		return nil, err
	}
	return out.order()
}

func (c *Client) GetOrder(ctx context.Context, id string) (*Order, error) {
	var out orderResponse
	if err := c.do(ctx, http.MethodGet, "/api/v1/warehouse_orders/"+url.PathEscape(id), nil, &out); err != nil {
		return nil, err
	}
	return out.order()
}

// orderResponse is how Theseus returns one order: wrapped in "warehouse_order"
// (app/views/api/v1/warehouse_orders/show.json.jb), on create and on get.
type orderResponse struct {
	WarehouseOrder *Order `json:"warehouse_order"`
}

func (r orderResponse) order() (*Order, error) {
	if r.WarehouseOrder == nil || r.WarehouseOrder.ID == "" {
		return nil, fmt.Errorf("theseus: response has no warehouse_order id")
	}
	return r.WarehouseOrder, nil
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("theseus %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("theseus %s %s: HTTP %d: %s", method, path, resp.StatusCode, truncate(string(data), 500))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(data, out)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// Stock is the warehouse's total for one item. InStock goes below zero when
// orders wait on a restock; Inbound is what's on order. Costs are the lowest
// and highest unit cost among its SKUs, as the API's decimal strings ("" if
// none).
type Stock struct {
	InStock int
	Inbound int
	MinCost string
	MaxCost string
}

// StockFor totals the given SKUs, each with its "<sku>/…" children, so a
// parent SKU ("Swa/Shirt/HC") counts every size under it. ListSKUs omits
// out-of-stock SKUs, so a missing SKU has none.
func StockFor(skus []SKU, want ...string) Stock {
	var st Stock
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, k := range skus {
		if !matchesAny(k.SKU, want) {
			continue
		}
		st.InStock += k.InStock
		if k.Inbound != nil {
			st.Inbound += *k.Inbound
		}
		c, err := strconv.ParseFloat(k.UnitCost, 64)
		if err != nil {
			continue
		}
		if c < lo {
			lo, st.MinCost = c, k.UnitCost
		}
		if c > hi {
			hi, st.MaxCost = c, k.UnitCost
		}
	}
	return st
}

func matchesAny(sku string, want []string) bool {
	for _, w := range want {
		if sku == w || strings.HasPrefix(sku, w+"/") {
			return true
		}
	}
	return false
}
