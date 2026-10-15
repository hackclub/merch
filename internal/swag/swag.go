// Package swag holds the pure business rules for swag requests: limits,
// cooldowns, international shipping, and request status transitions. It has no
// I/O so it can be tested exhaustively.
package swag

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
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

// Bindings are how a multi-page printed item is bound, for the 3D flip-through:
// "" is a single sheet, "staple" is saddle-stitched (folded sheets, stapled on
// the fold) and "perfect" is a glued square spine, like a paperback.
var Bindings = map[string]bool{"": true, "staple": true, "perfect": true}

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
	MinPerRequest int      `json:"min_per_request"` // fewest per request (all sizes together); 1 for most things
	QuickPicks    []int    `json:"quick_picks"`     // one-tap amounts on the shop, ascending; empty = worked out from the limits
	MaxPerUser    *int     `json:"max_per_user"`    // lifetime cap; nil = unlimited
	// Physical size in inches, for printed items; both nil when it doesn't apply.
	WidthIn  *float64 `json:"width_in"`
	HeightIn *float64 `json:"height_in"`
	// How it was printed: paper finish (see Finishes) and thickness in inches.
	Finish      string   `json:"finish"`
	ThicknessIn *float64 `json:"thickness_in"`
	// WeightOz is one unit's shipping weight in ounces; nil = worked out from
	// its paper size, if it has one (see Pack).
	WeightOz *float64 `json:"weight_oz"`
	// Real-world photos people have posted of it.
	Photos []Photo `json:"photos"`
	// Packs names how the item is packed (see Packs; "" = any quantity).
	// Quantities are limited to PackQuantity steps, and a pack line counts as
	// one item toward the per-request total.
	Packs string `json:"packs"`

	// Binding (see Bindings) and Pages, the page images of a booklet in reading
	// order: front cover first, back cover last.
	Binding string   `json:"binding"`
	Pages   []string `json:"pages"`
	// ShipsWith lists warehouse SKUs packed with every unit of this item, like
	// the intro card that goes with each magazine.
	ShipsWith []string `json:"ships_with"`
	// Clip is a short looping video or GIF of the real item (someone flipping
	// through it); the shop tile plays it instead of the 3D view.
	Clip string `json:"clip"`

	// ClubOnly items (the full-size magazine, the T-shirt) aren't offered for
	// personal requests, only to people running a club or an event.
	ClubOnly bool `json:"club_only"`

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

// Purpose is what a request is for; the request form asks it first.
type Purpose string

const (
	PurposePersonal Purpose = "personal" // for themselves and to share with friends
	PurposeClub     Purpose = "club"     // they run a Hack Club
	PurposeEvent    Purpose = "event"    // they run or exhibit at an event
)

// Purposes are the purposes a new request can have. Imported history has none ("").
var Purposes = map[Purpose]bool{PurposePersonal: true, PurposeClub: true, PurposeEvent: true}

// Offered reports whether an item can be requested for the given purpose.
func (it Item) Offered(p Purpose) bool {
	return p != PurposePersonal || !it.ClubOnly
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
	ErrBadPurpose     = errors.New("say what the request is for")
	ErrNoReason       = errors.New("tell us in a sentence or two how you'll use it")
)

type ValidateInput struct {
	Settings Settings
	Catalog  map[int64]Item
	Purpose  Purpose
	// Reason is how a club or event will use what it asks for (the request's
	// note); personal requests don't need one.
	Reason string
	Lines  []Line
	// Country is where it ships (alpha-2); outside the US, packages are capped
	// at 3 lb.
	Country string
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
	if !Purposes[in.Purpose] {
		return ErrBadPurpose
	}
	if in.Purpose != PurposePersonal && strings.TrimSpace(in.Reason) == "" {
		return ErrNoReason
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
	// A sized item can take one line per size; its limits count every size together.
	type key struct {
		item int64
		sku  string
	}
	seen := map[key]bool{}
	perItem := map[int64]int{}
	total := 0
	for _, l := range in.Lines {
		if l.Quantity < 1 {
			return ErrBadQuantity
		}
		item, ok := in.Catalog[l.ItemID]
		if !ok || !item.Visible {
			return ErrUnknownItem
		}
		if !item.Offered(in.Purpose) {
			return fmt.Errorf("%w: %s is only for clubs and events", ErrUnknownItem, item.Name)
		}
		k := key{l.ItemID, ""}
		if len(item.Variants) > 0 {
			k.sku = l.SKU
		}
		if seen[k] {
			return ErrDuplicateItem
		}
		seen[k] = true
		perItem[l.ItemID] += l.Quantity
		if len(item.Variants) > 0 {
			if _, ok := item.Variant(l.SKU); !ok {
				return fmt.Errorf("%w for %s", ErrBadVariant, item.Name)
			}
		} else if l.SKU != "" && l.SKU != item.SKU {
			return fmt.Errorf("%w: %s doesn't come in sizes", ErrBadVariant, item.Name)
		}
		if item.Packs != "" && !PackQuantity(item.Packs, l.Quantity) {
			return fmt.Errorf("%w: %s comes in %s", ErrBadQuantity, item.Name, PackSizes(item.Packs))
		}
		if perItem[l.ItemID] > item.MaxPerRequest {
			return fmt.Errorf("%w: at most %d %s per request", ErrItemLimit, item.MaxPerRequest, item.Name)
		}
		if item.MaxPerUser != nil && in.PriorQuantities[l.ItemID]+perItem[l.ItemID] > *item.MaxPerUser {
			return fmt.Errorf("%w: at most %d %s per person", ErrItemLimit, *item.MaxPerUser, item.Name)
		}
		if item.Packs != "" {
			total++
		} else {
			total += l.Quantity
		}
	}
	for id, q := range perItem {
		if item := in.Catalog[id]; q < item.MinPerRequest {
			return fmt.Errorf("%w: at least %d %s per request", ErrBadQuantity, item.MinPerRequest, item.Name)
		}
	}
	if _, err := Pack(in.Lines, in.Catalog, in.Country != "" && IsInternational(in.Country)); err != nil {
		return err
	}
	if in.Settings.MaxItemsPerRequest > 0 && total > in.Settings.MaxItemsPerRequest {
		return fmt.Errorf("%w (max %d)", ErrTooManyItems, in.Settings.MaxItemsPerRequest)
	}
	return nil
}

// Packs lists the small pack sizes for each kind of pack; past the last one,
// packs come in multiples of 50 ("fives" is every multiple of 5).
// web/src/lib/cart.ts mirrors this.
var Packs = map[string][]int{
	"fives":    {},
	"postcard": {5, 10, 15, 20, 25},
	"sticker":  {3, 10},
}

// PackQuantity reports whether q is an allowed quantity for an item in the
// given packs: multiples of 5 for "fives", or one of its small sizes / a
// multiple of 50 otherwise.
func PackQuantity(packs string, q int) bool {
	if packs == "fives" {
		return q > 0 && q%5 == 0
	}
	return slices.Contains(Packs[packs], q) || (q > 0 && q%50 == 0)
}

// PackSizes describes the allowed quantities, e.g. "3, 10, then multiples of 50".
func PackSizes(packs string) string {
	if packs == "fives" {
		return "multiples of 5"
	}
	var b strings.Builder
	for _, n := range Packs[packs] {
		fmt.Fprintf(&b, "%d, ", n)
	}
	return b.String() + "then multiples of 50"
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

// Shipped is a warehouse SKU going out in a box, and the request item it's for.
type Shipped struct {
	ItemID   int64
	SKU      string
	Quantity int
}

// Shipment is everything that goes in the box: each requested line, followed
// by what its item always ships with (one per unit). Repeated SKUs are merged
// into their first line, so the warehouse sees each SKU once.
func Shipment(lines []Shipped, shipsWith map[int64][]string) []Shipped {
	var out []Shipped
	at := map[string]int{}
	add := func(s Shipped) {
		if i, ok := at[s.SKU]; ok {
			out[i].Quantity += s.Quantity
			return
		}
		at[s.SKU] = len(out)
		out = append(out, s)
	}
	for _, l := range lines {
		add(l)
		for _, sku := range shipsWith[l.ItemID] {
			add(Shipped{ItemID: l.ItemID, SKU: sku, Quantity: l.Quantity})
		}
	}
	return out
}
