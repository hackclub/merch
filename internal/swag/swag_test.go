package swag

import (
	"errors"
	"slices"
	"testing"
	"time"
)

var now = time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

func settings() Settings {
	return Settings{
		RequestsOpen:                  true,
		MaxItemsPerRequest:            5,
		RequestCooldownDays:           30,
		InternationalShippingFeeCents: 1500,
	}
}

func catalog() map[int64]Item {
	return map[int64]Item{
		1: {ID: 1, SKU: "Sti/Pck/1", Name: "Sticker pack", Visible: true, MaxPerRequest: 3},
		2: {ID: 2, SKU: "Swa/Sox/1", Name: "Socks", Visible: true, MaxPerRequest: 1, MaxPerUser: intp(1)},
		3: {ID: 3, SKU: "Swa/Hid/1", Name: "Hidden hoodie", Visible: false, MaxPerRequest: 1},
		4: {ID: 4, SKU: "Swa/Shirt/HC", Name: "T-shirt", Visible: true, MaxPerRequest: 1, ClubOnly: true, Variants: []Variant{
			{Label: "S", SKU: "Swa/Shirt/HC/S"}, {Label: "M", SKU: "Swa/Shirt/HC/M"},
		}},
	}
}

func intp(i int) *int { return &i }

func TestValidateOK(t *testing.T) {
	err := Validate(ValidateInput{
		Settings: settings(),
		Catalog:  catalog(),
		Purpose:  PurposePersonal,
		Lines:    []Line{{ItemID: 1, Quantity: 3}, {ItemID: 2, Quantity: 1}},
		Now:      now,
	})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateErrors(t *testing.T) {
	last := now.Add(-10 * 24 * time.Hour)
	oldEnough := now.Add(-31 * 24 * time.Hour)
	closed := settings()
	closed.RequestsOpen = false

	cases := []struct {
		name string
		in   ValidateInput
		want error
	}{
		{"closed", ValidateInput{Settings: closed, Catalog: catalog(), Lines: []Line{{ItemID: 1, Quantity: 1}}}, ErrRequestsClosed},
		{"empty", ValidateInput{Settings: settings(), Catalog: catalog()}, ErrNoItems},
		{"zero qty", ValidateInput{Settings: settings(), Catalog: catalog(), Lines: []Line{{ItemID: 1, Quantity: 0}}}, ErrBadQuantity},
		{"unknown item", ValidateInput{Settings: settings(), Catalog: catalog(), Lines: []Line{{ItemID: 99, Quantity: 1}}}, ErrUnknownItem},
		{"hidden item", ValidateInput{Settings: settings(), Catalog: catalog(), Lines: []Line{{ItemID: 3, Quantity: 1}}}, ErrUnknownItem},
		{"duplicate line", ValidateInput{Settings: settings(), Catalog: catalog(), Lines: []Line{{ItemID: 1, Quantity: 1}, {ItemID: 1, Quantity: 1}}}, ErrDuplicateItem},
		{"over per-request", ValidateInput{Settings: settings(), Catalog: catalog(), Lines: []Line{{ItemID: 1, Quantity: 4}}}, ErrItemLimit},
		{"over per-user lifetime", ValidateInput{Settings: settings(), Catalog: catalog(), Lines: []Line{{ItemID: 2, Quantity: 1}}, PriorQuantities: map[int64]int{2: 1}}, ErrItemLimit},
		{"over total", func() ValidateInput {
			s := settings()
			s.MaxItemsPerRequest = 2
			return ValidateInput{Settings: s, Catalog: catalog(), Lines: []Line{{ItemID: 1, Quantity: 2}, {ItemID: 2, Quantity: 1}}}
		}(), ErrTooManyItems},
		{"size missing", ValidateInput{Settings: settings(), Catalog: catalog(), Lines: []Line{{ItemID: 4, Quantity: 1}}}, ErrBadVariant},
		{"size unknown", ValidateInput{Settings: settings(), Catalog: catalog(), Lines: []Line{{ItemID: 4, Quantity: 1, SKU: "Swa/Shirt/HC/XXL"}}}, ErrBadVariant},
		{"size on sizeless item", ValidateInput{Settings: settings(), Catalog: catalog(), Lines: []Line{{ItemID: 1, Quantity: 1, SKU: "Swa/Shirt/HC/S"}}}, ErrBadVariant},
		{"cooldown", ValidateInput{Settings: settings(), Catalog: catalog(), Lines: []Line{{ItemID: 1, Quantity: 1}}, LastRequestAt: &last, Now: now}, ErrCooldown},
		{"no purpose", ValidateInput{Settings: settings(), Catalog: catalog(), Purpose: "none", Lines: []Line{{ItemID: 1, Quantity: 1}}}, ErrBadPurpose},
		{"unknown purpose", ValidateInput{Settings: settings(), Catalog: catalog(), Purpose: "resale", Lines: []Line{{ItemID: 1, Quantity: 1}}}, ErrBadPurpose},
		{"club request without a reason", ValidateInput{Settings: settings(), Catalog: catalog(), Purpose: PurposeClub, Reason: "  ", Lines: []Line{{ItemID: 1, Quantity: 1}}}, ErrNoReason},
		{"event request without a reason", ValidateInput{Settings: settings(), Catalog: catalog(), Purpose: PurposeEvent, Reason: "none", Lines: []Line{{ItemID: 1, Quantity: 1}}}, ErrNoReason},
		{"club-only item for personal use", ValidateInput{Settings: settings(), Catalog: catalog(), Purpose: PurposePersonal, Lines: []Line{{ItemID: 4, Quantity: 1, SKU: "Swa/Shirt/HC/M"}}}, ErrUnknownItem},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.in.Now.IsZero() {
				tc.in.Now = now
			}
			switch tc.in.Purpose {
			case "":
				tc.in.Purpose = PurposeClub
			case "none":
				tc.in.Purpose = ""
			}
			switch tc.in.Reason {
			case "":
				tc.in.Reason = "Our club fair on Friday."
			case "none":
				tc.in.Reason = ""
			}
			err := Validate(tc.in)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}

	t.Run("cooldown elapsed", func(t *testing.T) {
		err := Validate(ValidateInput{Settings: settings(), Catalog: catalog(), Purpose: PurposeEvent, Reason: "Exhibiting at a maker faire, ~500 people.", Lines: []Line{{ItemID: 1, Quantity: 1}}, LastRequestAt: &oldEnough, Now: now})
		if err != nil {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestValidateVariants(t *testing.T) {
	err := Validate(ValidateInput{
		Settings: settings(),
		Catalog:  catalog(),
		Purpose:  PurposeClub,
		Reason:   "Shirts for our club officers.",
		Lines:    []Line{{ItemID: 4, Quantity: 1, SKU: "Swa/Shirt/HC/M"}, {ItemID: 1, Quantity: 1, SKU: "Sti/Pck/1"}},
		Now:      now,
	})
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestLineSKU(t *testing.T) {
	cat := catalog()
	if v, ok := cat[4].Variant("Swa/Shirt/HC/M"); !ok || v.Label != "M" {
		t.Errorf("Variant(M) = %+v, %v", v, ok)
	}
	if _, ok := cat[4].Variant("Swa/Shirt/HC"); ok {
		t.Error("the parent SKU is not a variant")
	}
	if _, ok := cat[1].Variant(""); ok {
		t.Error("sizeless items have no variants")
	}
}

func TestIsInternational(t *testing.T) {
	for country, want := range map[string]bool{"US": false, "us": false, " US ": false, "CA": true, "IN": true} {
		if got := IsInternational(country); got != want {
			t.Errorf("IsInternational(%q) = %v, want %v", country, got, want)
		}
	}
}

func TestInitialStatus(t *testing.T) {
	st, fee := InitialStatus(settings(), "US")
	if st != StatusPending || fee != 0 {
		t.Errorf("US: got %s/%d", st, fee)
	}
	st, fee = InitialStatus(settings(), "DE")
	if st != StatusAwaitingPayment || fee != 1500 {
		t.Errorf("DE: got %s/%d", st, fee)
	}
	free := settings()
	free.InternationalShippingFeeCents = 0
	if st, _ := InitialStatus(free, "DE"); st != StatusPending {
		t.Errorf("free international shipping should skip payment, got %s", st)
	}
}

func TestValidAddress(t *testing.T) {
	good := Address{FirstName: "Orpheus", LastName: "Dino", Line1: "8605 Santa Monica Blvd", City: "West Hollywood", State: "CA", PostalCode: "90069", Country: "US"}
	if err := good.Validate(); err != nil {
		t.Fatalf("good address: %v", err)
	}
	bad := good
	bad.Line1 = " "
	if err := bad.Validate(); err == nil {
		t.Error("missing line1 should fail")
	}
	bad = good
	bad.Country = "USA"
	if err := bad.Validate(); err == nil {
		t.Error("country must be ISO alpha-2")
	}
	intl := good
	intl.Country = "DE"
	if err := intl.Validate(); err == nil {
		t.Error("international address without phone should fail (customs)")
	}
	intl.Phone = "+49 30 1234567"
	if err := intl.Validate(); err != nil {
		t.Errorf("international with phone: %v", err)
	}
}

func TestCanTransition(t *testing.T) {
	ok := [][2]Status{
		{StatusAwaitingPayment, StatusPending},
		{StatusAwaitingPayment, StatusCancelled},
		{StatusPending, StatusDispatched},
		{StatusPending, StatusRejected},
		{StatusPending, StatusCancelled},
	}
	for _, p := range ok {
		if !CanTransition(p[0], p[1]) {
			t.Errorf("%s -> %s should be allowed", p[0], p[1])
		}
	}
	bad := [][2]Status{
		{StatusAwaitingPayment, StatusDispatched},
		{StatusDispatched, StatusCancelled},
		{StatusRejected, StatusPending},
	}
	for _, p := range bad {
		if CanTransition(p[0], p[1]) {
			t.Errorf("%s -> %s should be refused", p[0], p[1])
		}
	}
}

func TestPackQuantity(t *testing.T) {
	for _, c := range []struct {
		packs string
		q     int
		want  bool
	}{
		{"fives", 5, true}, {"fives", 10, true}, {"fives", 15, true}, {"fives", 30, true}, {"fives", 100, true},
		{"fives", 0, false}, {"fives", -5, false}, {"fives", 1, false}, {"fives", 3, false}, {"fives", 12, false},
		{"postcard", 5, true}, {"postcard", 10, true}, {"postcard", 25, true}, {"postcard", 50, true}, {"postcard", 150, true}, {"postcard", 1000, true},
		{"postcard", 0, false}, {"postcard", 1, false}, {"postcard", 3, false}, {"postcard", 30, false}, {"postcard", 75, false}, {"postcard", 125, false},
		{"sticker", 3, true}, {"sticker", 10, true}, {"sticker", 50, true}, {"sticker", 100, true}, {"sticker", 150, true},
		{"sticker", 0, false}, {"sticker", 1, false}, {"sticker", 5, false}, {"sticker", 20, false}, {"sticker", 25, false}, {"sticker", 60, false},
	} {
		if got := PackQuantity(c.packs, c.q); got != c.want {
			t.Errorf("PackQuantity(%q, %d) = %v, want %v", c.packs, c.q, got, c.want)
		}
	}
}

func TestPackSizes(t *testing.T) {
	if got := PackSizes("fives"); got != "multiples of 5" {
		t.Errorf("PackSizes(fives) = %q", got)
	}
	if got := PackSizes("sticker"); got != "3, 10, then multiples of 50" {
		t.Errorf("PackSizes(sticker) = %q", got)
	}
	if got := PackSizes("postcard"); got != "5, 10, 15, 20, 25, then multiples of 50" {
		t.Errorf("PackSizes(postcard) = %q", got)
	}
}

func TestValidatePacks(t *testing.T) {
	cat := catalog()
	cat[5] = Item{ID: 5, SKU: "Pri/Wel/4x6/1st", Name: "Postcard", Visible: true, MaxPerRequest: 200, Packs: "postcard"}
	cat[6] = Item{ID: 6, SKU: "Sti/Sti/Kaw/1st", Name: "Kawaii sticker", Visible: true, MaxPerRequest: 150, Packs: "sticker"}
	cat[7] = Item{ID: 7, SKU: "Pri/Bok/Mini25/1st", Name: "Mini-Magazine", Visible: true, MaxPerRequest: 100, Packs: "fives"}
	in := func(id int64, q int) ValidateInput {
		return ValidateInput{Settings: settings(), Catalog: cat, Purpose: PurposePersonal, Lines: []Line{{ItemID: id, Quantity: q}, {ItemID: 1, Quantity: 3}}, Now: now}
	}
	// A pack counts as one item toward the per-request total (5 here).
	for id, qs := range map[int64][]int{5: {5, 25, 50, 150}, 6: {3, 10, 50, 150}, 7: {5, 10, 15, 30, 100}} {
		for _, q := range qs {
			if err := Validate(in(id, q)); err != nil {
				t.Errorf("item %d × %d: %v", id, q, err)
			}
		}
	}
	for id, qs := range map[int64][]int{5: {1, 3, 7, 30, 75}, 6: {1, 5, 25, 75}, 7: {1, 3, 7, 12}} {
		for _, q := range qs {
			if err := Validate(in(id, q)); !errors.Is(err, ErrBadQuantity) {
				t.Errorf("item %d × %d: err = %v, want ErrBadQuantity", id, q, err)
			}
		}
	}
}

func TestShipment(t *testing.T) {
	// The magazine always ships with its intro card, one per magazine; a card
	// requested on its own merges into the same contents line.
	shipsWith := map[int64][]string{7: {"Pri/M25/4x6/1st"}}
	got := Shipment([]Shipped{
		{ItemID: 1, SKU: "Sti/Pck/1", Quantity: 3},
		{ItemID: 7, SKU: "Pri/Bok/M25/1st", Quantity: 2},
		{ItemID: 8, SKU: "Pri/M25/4x6/1st", Quantity: 1},
	}, shipsWith)
	want := []Shipped{
		{ItemID: 1, SKU: "Sti/Pck/1", Quantity: 3},
		{ItemID: 7, SKU: "Pri/Bok/M25/1st", Quantity: 2},
		{ItemID: 7, SKU: "Pri/M25/4x6/1st", Quantity: 3},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Shipment = %+v, want %+v", got, want)
	}
	if got := Shipment([]Shipped{{ItemID: 1, SKU: "Sti/Pck/1", Quantity: 1}}, nil); len(got) != 1 {
		t.Fatalf("plain shipment = %+v", got)
	}
}

func TestBindings(t *testing.T) {
	for _, b := range []string{"", "staple", "perfect"} {
		if !Bindings[b] {
			t.Errorf("binding %q should be valid", b)
		}
	}
	if Bindings["spiral"] {
		t.Error("spiral isn't rendered")
	}
}

func TestValidateSeveralSizes(t *testing.T) {
	cat := catalog()
	shirt := cat[4]
	shirt.MaxPerRequest, shirt.MaxPerUser = 3, intp(3)
	cat[4] = shirt
	in := func(prior int, lines ...Line) ValidateInput {
		return ValidateInput{Settings: settings(), Catalog: cat, Purpose: PurposeClub, Reason: "Shirts for our club officers.",
			Lines: lines, PriorQuantities: map[int64]int{4: prior}, Now: now}
	}
	s := func(size string, q int) Line { return Line{ItemID: 4, Quantity: q, SKU: "Swa/Shirt/HC/" + size} }

	if err := Validate(in(0, s("S", 1), s("M", 2))); err != nil {
		t.Fatalf("two sizes of one item: %v", err)
	}
	if err := Validate(in(0, s("M", 1), s("M", 1))); !errors.Is(err, ErrDuplicateItem) {
		t.Fatalf("same size twice: err = %v, want ErrDuplicateItem", err)
	}
	// Limits count every size of the item together.
	if err := Validate(in(0, s("S", 2), s("M", 2))); !errors.Is(err, ErrItemLimit) {
		t.Fatalf("over the per-request max across sizes: err = %v, want ErrItemLimit", err)
	}
	if err := Validate(in(2, s("S", 1), s("M", 1))); !errors.Is(err, ErrItemLimit) {
		t.Fatalf("over the per-person max across sizes: err = %v, want ErrItemLimit", err)
	}
}
