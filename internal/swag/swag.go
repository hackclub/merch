// Package swag holds the pure business rules for swag requests: limits,
// cooldowns, international shipping, and request status transitions. It has no
// I/O so it can be tested exhaustively.
package swag

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

type Status string

const (
	StatusAwaitingPayment Status = "awaiting_payment" // international: waiting on HCB shipping payment
	StatusPending         Status = "pending"          // waiting on admin review
	StatusDispatched      Status = "dispatched"       // order created in mail.hackclub.com
	StatusRejected        Status = "rejected"
	StatusCancelled       Status = "cancelled"
)

// Settings are the admin-managed global limits.
type Settings struct {
	RequestsOpen                  bool `json:"requests_open"`
	MaxItemsPerRequest            int  `json:"max_items_per_request"`
	RequestCooldownDays           int  `json:"request_cooldown_days"`
	InternationalShippingFeeCents int  `json:"international_shipping_fee_cents"`
}

// Finishes are the paper surfaces the 3D preview knows how to render.
var Finishes = map[string]bool{"": true, "matte": true, "gloss": true, "linen": true, "uncoated": true}

// Photo is a real-world picture of an item, credited to whoever took it.
type Photo struct {
	URL     string `json:"url"`
	Caption string `json:"caption"`
	Credit  string `json:"credit"`
}

// Item is a listed catalog item backed by a warehouse SKU.
type Item struct {
	ID            int64    `json:"id"`
	SKU           string   `json:"sku"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Images        []string `json:"images"` // CDN URLs, in display order; first is the thumbnail
	Visible       bool     `json:"visible"`
	SortOrder     int      `json:"sort_order"`
	MaxPerRequest int      `json:"max_per_request"`
	MaxPerUser    *int     `json:"max_per_user"` // lifetime cap; nil = unlimited
	// Physical size in inches, for printed items; both nil when it doesn't apply.
	WidthIn  *float64 `json:"width_in"`
	HeightIn *float64 `json:"height_in"`
	// How it was printed: paper finish (see Finishes) and thickness in inches.
	Finish      string   `json:"finish"`
	ThicknessIn *float64 `json:"thickness_in"`
	// Real-world photos people have posted of it.
	Photos []Photo `json:"photos"`
	// SoldInPacks limits quantities to PackQuantity steps (postcards: 5, 10…25,
	// then 50, 100, 150…). A pack line counts as one item toward the
	// per-request total.
	SoldInPacks bool `json:"sold_in_packs"`

	// Variants are the sizes of an item, each its own warehouse SKU. When an
	// item has variants, every request line for it must pick one; limits still
	// apply to the item as a whole.
	Variants []Variant `json:"variants"`
}

// Variant is one size of an item (e.g. "M" -> "Swa/Shirt/HC/M").
type Variant struct {
	Label string `json:"label"`
	SKU   string `json:"sku"`
}

// Variant returns the item's variant with the given SKU.
func (it Item) Variant(sku string) (Variant, bool) {
	for _, v := range it.Variants {
		if v.SKU == sku {
			return v, true
		}
	}
	return Variant{}, false
}

type Line struct {
	ItemID   int64  `json:"item_id"`
	Quantity int    `json:"quantity"`
	SKU      string `json:"sku,omitempty"` // the chosen variant's SKU; required when the item has variants
}

type Address struct {
	FirstName  string `json:"first_name"`
	LastName   string `json:"last_name"`
	Line1      string `json:"line_1"`
	Line2      string `json:"line_2"`
	City       string `json:"city"`
	State      string `json:"state"`
	PostalCode string `json:"postal_code"`
	Country    string `json:"country"` // ISO 3166-1 alpha-2
	Phone      string `json:"phone_number"`
}

var (
	ErrRequestsClosed = errors.New("requests are currently closed")
	ErrNoItems        = errors.New("pick at least one item")
	ErrBadQuantity    = errors.New("quantity must be at least 1")
	ErrUnknownItem    = errors.New("item is not available")
	ErrDuplicateItem  = errors.New("item listed twice")
	ErrBadVariant     = errors.New("pick a size")
	ErrItemLimit      = errors.New("item limit exceeded")
	ErrTooManyItems   = errors.New("too many items in one request")
	ErrCooldown       = errors.New("you requested swag recently; try again later")
	ErrBadAddress     = errors.New("invalid address")
)

type ValidateInput struct {
	Settings Settings
	Catalog  map[int64]Item
	Lines    []Line
	// PriorQuantities is how many of each item the user has already received
	// or has in flight (non-rejected, non-cancelled requests).
	PriorQuantities map[int64]int
	// LastRequestAt is the user's most recent non-rejected, non-cancelled request.
	LastRequestAt *time.Time
	Now           time.Time
}

func Validate(in ValidateInput) error {
	if !in.Settings.RequestsOpen {
		return ErrRequestsClosed
	}
	if len(in.Lines) == 0 {
		return ErrNoItems
	}
	if in.LastRequestAt != nil && in.Settings.RequestCooldownDays > 0 {
		next := in.LastRequestAt.Add(time.Duration(in.Settings.RequestCooldownDays) * 24 * time.Hour)
		if in.Now.Before(next) {
			return fmt.Errorf("%w (next request allowed %s)", ErrCooldown, next.Format("2006-01-02"))
		}
	}
	seen := map[int64]bool{}
	total := 0
	for _, l := range in.Lines {
		if l.Quantity < 1 {
			return ErrBadQuantity
		}
		item, ok := in.Catalog[l.ItemID]
		if !ok || !item.Visible {
			return ErrUnknownItem
		}
		if seen[l.ItemID] {
			return ErrDuplicateItem
		}
		seen[l.ItemID] = true
		if len(item.Variants) > 0 {
			if _, ok := item.Variant(l.SKU); !ok {
				return fmt.Errorf("%w for %s", ErrBadVariant, item.Name)
			}
		} else if l.SKU != "" && l.SKU != item.SKU {
			return fmt.Errorf("%w: %s doesn't come in sizes", ErrBadVariant, item.Name)
		}
		if item.SoldInPacks && !PackQuantity(l.Quantity) {
			return fmt.Errorf("%w: %s comes in 5, 10, 15, 20, 25, then multiples of 50", ErrBadQuantity, item.Name)
		}
		if l.Quantity > item.MaxPerRequest {
			return fmt.Errorf("%w: at most %d %s per request", ErrItemLimit, item.MaxPerRequest, item.Name)
		}
		if item.MaxPerUser != nil && in.PriorQuantities[l.ItemID]+l.Quantity > *item.MaxPerUser {
			return fmt.Errorf("%w: at most %d %s per person", ErrItemLimit, *item.MaxPerUser, item.Name)
		}
		if item.SoldInPacks {
			total++
		} else {
			total += l.Quantity
		}
	}
	if in.Settings.MaxItemsPerRequest > 0 && total > in.Settings.MaxItemsPerRequest {
		return fmt.Errorf("%w (max %d)", ErrTooManyItems, in.Settings.MaxItemsPerRequest)
	}
	return nil
}

// PackQuantity reports whether q is an allowed quantity for an item sold in
// packs: 5 to 25 in steps of 5, then multiples of 50.
func PackQuantity(q int) bool {
	if q <= 25 {
		return q > 0 && q%5 == 0
	}
	return q%50 == 0
}

// IsInternational reports whether shipping to country (alpha-2) needs the
// recipient to cover shipping. Hack Club HQ covers domestic US shipping.
func IsInternational(country string) bool {
	return strings.ToUpper(strings.TrimSpace(country)) != "US"
}

// InitialStatus decides where a new request starts and what shipping it owes.
func InitialStatus(s Settings, country string) (Status, int) {
	if IsInternational(country) && s.InternationalShippingFeeCents > 0 {
		return StatusAwaitingPayment, s.InternationalShippingFeeCents
	}
	return StatusPending, 0
}

var alpha2 = regexp.MustCompile(`^[A-Z]{2}$`)

func (a Address) Validate() error {
	req := map[string]string{
		"first_name": a.FirstName, "last_name": a.LastName, "line_1": a.Line1,
		"city": a.City, "postal_code": a.PostalCode, "country": a.Country,
	}
	for k, v := range req {
		if strings.TrimSpace(v) == "" {
			return fmt.Errorf("%w: %s is required", ErrBadAddress, k)
		}
	}
	if !alpha2.MatchString(a.Country) {
		return fmt.Errorf("%w: country must be a 2-letter ISO code", ErrBadAddress)
	}
	if IsInternational(a.Country) && strings.TrimSpace(a.Phone) == "" {
		return fmt.Errorf("%w: phone_number is required for international shipping (customs)", ErrBadAddress)
	}
	return nil
}

var transitions = map[Status][]Status{
	StatusAwaitingPayment: {StatusPending, StatusCancelled, StatusRejected},
	StatusPending:         {StatusDispatched, StatusRejected, StatusCancelled},
}

func CanTransition(from, to Status) bool {
	for _, s := range transitions[from] {
		if s == to {
			return true
		}
	}
	return false
}
