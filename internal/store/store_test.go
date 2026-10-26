package store

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/hackclub/merch/internal/auth"
	"github.com/hackclub/merch/internal/db/dbtest"
	"github.com/hackclub/merch/internal/swag"
	"github.com/hackclub/merch/internal/theseus"
)

var ctx = context.Background()

func newStore(t *testing.T) *Store { return New(dbtest.New(t)) }

func mkUser(t *testing.T, s *Store, hcaID, email string) *User {
	t.Helper()
	u, err := s.UpsertUser(ctx, auth.Identity{ID: hcaID, Email: email, Name: "Test " + hcaID})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func mkItem(t *testing.T, s *Store, sku string, visible bool) swag.Item {
	t.Helper()
	it, err := s.CreateItem(ctx, swag.Item{SKU: sku, Name: sku, Visible: visible, MaxPerRequest: 2})
	if err != nil {
		t.Fatal(err)
	}
	return it
}

var addr = swag.Address{FirstName: "O", LastName: "D", Line1: "1 Main", City: "Burlington", State: "VT", PostalCode: "05401", Country: "US"}

func TestUpsertUser(t *testing.T) {
	s := newStore(t)
	u1 := mkUser(t, s, "ident!1", "a@x.com")
	u2, err := s.UpsertUser(ctx, auth.Identity{ID: "ident!1", Email: "new@x.com", Name: "Renamed", SlackID: "U9"})
	if err != nil {
		t.Fatal(err)
	}
	if u1.ID != u2.ID || u2.Email != "new@x.com" || u2.Name != "Renamed" || u2.SlackID != "U9" {
		t.Fatalf("upsert did not update in place: %+v -> %+v", u1, u2)
	}
}

func TestSessions(t *testing.T) {
	s := newStore(t)
	u := mkUser(t, s, "ident!1", "a@x.com")
	tok, err := s.CreateSession(ctx, u.ID, time.Hour)
	if err != nil || len(tok) < 32 {
		t.Fatalf("token %q err %v", tok, err)
	}
	got, err := s.UserBySession(ctx, tok)
	if err != nil || got.ID != u.ID {
		t.Fatalf("UserBySession = %+v, %v", got, err)
	}
	if _, err := s.UserBySession(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown token err = %v", err)
	}
	if err := s.DeleteSession(ctx, tok); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UserBySession(ctx, tok); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted token err = %v", err)
	}
	expired, _ := s.CreateSession(ctx, u.ID, -time.Minute)
	if _, err := s.UserBySession(ctx, expired); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired token err = %v", err)
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	s := newStore(t)
	got, err := s.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !got.RequestsOpen || got.InternationalShippingFeeCents != 1500 {
		t.Fatalf("defaults = %+v", got)
	}
	want := swag.Settings{RequestsOpen: false, InternationalShippingFeeCents: 999}
	if err := s.UpdateSettings(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetSettings(ctx)
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestItems(t *testing.T) {
	s := newStore(t)
	a := mkItem(t, s, "Sti/A", true)
	mkItem(t, s, "Sti/B", false)
	if _, err := s.CreateItem(ctx, swag.Item{SKU: "Sti/A", Name: "dup", MaxPerRequest: 1}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate sku err = %v", err)
	}

	visible, _ := s.ListItems(ctx, true)
	all, _ := s.ListItems(ctx, false)
	if len(visible) != 1 || len(all) != 2 {
		t.Fatalf("visible=%d all=%d", len(visible), len(all))
	}

	five := 5
	a.Name, a.MaxPerUser, a.SortOrder, a.ClubOnly, a.StaffOnly, a.Model = "Renamed", &five, 3, true, true, "https://cdn.example/orpheus.glb"
	upd, err := s.UpdateItem(ctx, a)
	if err != nil || upd.Name != "Renamed" || upd.MaxPerUser == nil || *upd.MaxPerUser != 5 || !upd.ClubOnly || !upd.StaffOnly || upd.Model != "https://cdn.example/orpheus.glb" {
		t.Fatalf("UpdateItem = %+v, %v", upd, err)
	}
	if _, err := s.UpdateItem(ctx, swag.Item{ID: 999, SKU: "x", Name: "x", MaxPerRequest: 1}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing item err = %v", err)
	}
}

func TestItemImages(t *testing.T) {
	s := newStore(t)
	it := mkItem(t, s, "Pri/A", true)
	if it.Images == nil || len(it.Images) != 0 {
		t.Fatalf("new item images = %#v, want empty non-nil", it.Images)
	}

	it, err := s.AddItemImage(ctx, it.ID, "https://cdn.example/front.jpg")
	if err != nil {
		t.Fatal(err)
	}
	it, _ = s.AddItemImage(ctx, it.ID, "https://cdn.example/back.jpg")
	if want := []string{"https://cdn.example/front.jpg", "https://cdn.example/back.jpg"}; !slices.Equal(it.Images, want) {
		t.Fatalf("images = %v, want %v", it.Images, want)
	}

	// Reordering and removal go through UpdateItem.
	it.Images = []string{"https://cdn.example/back.jpg"}
	upd, err := s.UpdateItem(ctx, it)
	if err != nil || !slices.Equal(upd.Images, it.Images) {
		t.Fatalf("UpdateItem images = %v, %v", upd.Images, err)
	}
	listed, _ := s.ListItems(ctx, true)
	if !slices.Equal(listed[0].Images, it.Images) {
		t.Fatalf("listed images = %v", listed[0].Images)
	}

	w, h := 11.0, 17.0
	it.WidthIn, it.HeightIn = &w, &h
	upd, err = s.UpdateItem(ctx, it)
	if err != nil || upd.WidthIn == nil || *upd.WidthIn != 11 || *upd.HeightIn != 17 {
		t.Fatalf("UpdateItem size = %v x %v, %v", upd.WidthIn, upd.HeightIn, err)
	}

	thick := 0.016
	it.Finish, it.ThicknessIn = "linen", &thick
	it.Photos = []swag.Photo{{URL: "https://cdn.example/booth.jpg", Caption: "At a booth", Credit: "Boa_N"}}
	upd, err = s.UpdateItem(ctx, it)
	if err != nil || upd.Finish != "linen" || *upd.ThicknessIn != 0.016 || len(upd.Photos) != 1 || upd.Photos[0] != it.Photos[0] {
		t.Fatalf("UpdateItem print details = %+v, %v", upd, err)
	}
	it.Packs = "sticker"
	if upd, err = s.UpdateItem(ctx, it); err != nil || upd.Packs != "sticker" {
		t.Fatalf("UpdateItem packs = %+v, %v", upd, err)
	}
	upd, err = s.AddItemPhoto(ctx, it.ID, swag.Photo{URL: "https://cdn.example/stack.jpg", Credit: "Ryaan"})
	if err != nil || len(upd.Photos) != 2 || upd.Photos[1].Credit != "Ryaan" {
		t.Fatalf("AddItemPhoto = %+v, %v", upd.Photos, err)
	}
	if fresh := mkItem(t, s, "Pri/New", true); fresh.Photos == nil {
		t.Fatal("new item photos should be an empty list, not null")
	}
	it.Photos = upd.Photos
	it.PrintFiles = []swag.PrintFile{{URL: "https://cdn.example/front.pdf", Label: "Front", Bytes: 1200}}
	if upd, err = s.UpdateItem(ctx, it); err != nil || len(upd.PrintFiles) != 1 || upd.PrintFiles[0] != it.PrintFiles[0] {
		t.Fatalf("UpdateItem print files = %+v, %v", upd.PrintFiles, err)
	}
	upd, err = s.AddItemPrintFile(ctx, it.ID, swag.PrintFile{URL: "https://cdn.example/back.pdf", Label: "Back", Bytes: 900})
	if err != nil || len(upd.PrintFiles) != 2 || upd.PrintFiles[1].Label != "Back" {
		t.Fatalf("AddItemPrintFile = %+v, %v", upd.PrintFiles, err)
	}
	if fresh := mkItem(t, s, "Pri/Newer", true); fresh.PrintFiles == nil {
		t.Fatal("new item print files should be an empty list, not null")
	}

	if _, err := s.AddItemImage(ctx, 999, "https://cdn.example/x.jpg"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing item err = %v", err)
	}
}

var shirtSizes = []swag.Variant{{Label: "S", SKU: "Swa/Shirt/HC/S"}, {Label: "M", SKU: "Swa/Shirt/HC/M"}, {Label: "L", SKU: "Swa/Shirt/HC/L"}}

func TestItemVariants(t *testing.T) {
	s := newStore(t)
	plain := mkItem(t, s, "Sti/A", true)
	if plain.Variants == nil || len(plain.Variants) != 0 {
		t.Fatalf("new item variants = %#v, want empty non-nil", plain.Variants)
	}
	shirt, err := s.CreateItem(ctx, swag.Item{SKU: "Swa/Shirt/HC", Name: "T-shirt", Visible: true, MaxPerRequest: 1, Variants: shirtSizes})
	if err != nil || !slices.Equal(shirt.Variants, shirtSizes) {
		t.Fatalf("CreateItem variants = %+v, %v", shirt.Variants, err)
	}
	shirt.Variants = shirtSizes[:2]
	if upd, err := s.UpdateItem(ctx, shirt); err != nil || !slices.Equal(upd.Variants, shirtSizes[:2]) {
		t.Fatalf("UpdateItem variants = %+v, %v", upd.Variants, err)
	}

	// A SKU belongs to one item: no second item can claim a size another item sells.
	if _, err := s.CreateItem(ctx, swag.Item{SKU: "Swa/Shirt/Other", Name: "x", MaxPerRequest: 1, Variants: shirtSizes[1:2]}); !errors.Is(err, ErrConflict) {
		t.Fatalf("variant taken by another item: err = %v", err)
	}
	if _, err := s.CreateItem(ctx, swag.Item{SKU: "Swa/Shirt/HC/M", Name: "x", MaxPerRequest: 1}); !errors.Is(err, ErrConflict) {
		t.Fatalf("item SKU that is another item's size: err = %v", err)
	}
}

func TestVariantRequestLines(t *testing.T) {
	s := newStore(t)
	u := mkUser(t, s, "ident!1", "a@x.com")
	shirt, _ := s.CreateItem(ctx, swag.Item{SKU: "Swa/Shirt/HC", Name: "T-shirt", Visible: true, MaxPerRequest: 1, Variants: shirtSizes})
	sticker := mkItem(t, s, "Sti/A", true)
	r, err := s.CreateRequest(ctx, NewRequest{UserID: u.ID, Address: addr, Status: swag.StatusPending,
		Lines: []swag.Line{{ItemID: shirt.ID, Quantity: 1, SKU: "Swa/Shirt/HC/M"}, {ItemID: sticker.ID, Quantity: 2}}}, noCheck)
	if err != nil {
		t.Fatal(err)
	}
	want := []RequestLine{
		{ItemID: sticker.ID, SKU: "Sti/A", Name: "Sti/A", Quantity: 2},
		{ItemID: shirt.ID, SKU: "Swa/Shirt/HC/M", Name: "T-shirt", Variant: "M", Quantity: 1},
	}
	if !slices.Equal(r.Lines, want) {
		t.Fatalf("lines = %+v, want %+v", r.Lines, want)
	}
	s.CreateRequest(ctx, NewRequest{UserID: u.ID, Address: addr, Status: swag.StatusPending, Lines: []swag.Line{{ItemID: shirt.ID, Quantity: 1, SKU: "Swa/Shirt/HC/L"}}},
		func(prior map[int64]int) error {
			if prior[shirt.ID] != 1 {
				t.Errorf("sizes count toward the item's limit: prior = %v", prior)
			}
			return nil
		})
}

// Listing per-size warehouse SKUs as separate items (as the importer does) and
// then giving one item those sizes folds the others into it, history included.
func TestVariantsAbsorbPerSizeItems(t *testing.T) {
	s := newStore(t)
	u := mkUser(t, s, "ident!1", "a@x.com")
	m := mkItem(t, s, "Swa/Shirt/HC/M", false)
	small := mkItem(t, s, "Swa/Shirt/HC/S", false)
	keep := mkItem(t, s, "Sti/A", true)
	old, _ := s.CreateRequest(ctx, NewRequest{UserID: u.ID, Address: addr, Status: swag.StatusPending,
		Lines: []swag.Line{{ItemID: m.ID, Quantity: 1}, {ItemID: small.ID, Quantity: 1}, {ItemID: keep.ID, Quantity: 1}}}, noCheck)

	m.SKU, m.Name, m.Variants = "Swa/Shirt/HC", "T-shirt", shirtSizes
	shirt, err := s.UpdateItem(ctx, m)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetItem(ctx, small.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("absorbed item should be gone: %v", err)
	}
	got, _ := s.GetRequest(ctx, old.ID)
	want := []RequestLine{
		{ItemID: keep.ID, SKU: "Sti/A", Name: "Sti/A", Quantity: 1},
		{ItemID: shirt.ID, SKU: "Swa/Shirt/HC/M", Name: "T-shirt", Variant: "M", Quantity: 1},
		{ItemID: shirt.ID, SKU: "Swa/Shirt/HC/S", Name: "T-shirt", Variant: "S", Quantity: 1},
	}
	if !slices.Equal(got.Lines, want) {
		t.Fatalf("lines = %+v, want %+v", got.Lines, want)
	}
	s.CreateRequest(ctx, NewRequest{UserID: u.ID, Address: addr, Status: swag.StatusPending, Lines: []swag.Line{{ItemID: shirt.ID, Quantity: 1, SKU: "Swa/Shirt/HC/L"}}},
		func(prior map[int64]int) error {
			if prior[shirt.ID] != 2 {
				t.Errorf("absorbed history counts toward the item: prior = %v", prior)
			}
			return nil
		})
}

func TestCreateRequestPassesUsageToValidator(t *testing.T) {
	s := newStore(t)
	u := mkUser(t, s, "ident!1", "a@x.com")
	it := mkItem(t, s, "Sti/A", true)

	r1, err := s.CreateRequest(ctx, NewRequest{UserID: u.ID, Address: addr, Purpose: swag.PurposeClub, Lines: []swag.Line{{ItemID: it.ID, Quantity: 2}}, Status: swag.StatusPending},
		func(prior map[int64]int) error {
			if len(prior) != 0 {
				t.Errorf("first request saw prior=%v", prior)
			}
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if r1.Status != swag.StatusPending || r1.Purpose != swag.PurposeClub || len(r1.Lines) != 1 || r1.Lines[0].SKU != "Sti/A" || r1.Lines[0].Quantity != 2 {
		t.Fatalf("request = %+v", r1)
	}

	var sawPrior map[int64]int
	boom := errors.New("rejected by rules")
	_, err = s.CreateRequest(ctx, NewRequest{UserID: u.ID, Address: addr, Lines: []swag.Line{{ItemID: it.ID, Quantity: 1}}, Status: swag.StatusPending},
		func(prior map[int64]int) error {
			sawPrior = prior
			return boom
		})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if sawPrior[it.ID] != 2 {
		t.Fatalf("prior=%v", sawPrior)
	}
	mine, _ := s.ListRequestsByUser(ctx, u.ID)
	if len(mine) != 1 {
		t.Fatalf("validator error must roll back; have %d requests", len(mine))
	}

	// Rejected requests don't count toward usage.
	if _, err := s.TransitionRequest(ctx, r1.ID, swag.StatusRejected, "nope"); err != nil {
		t.Fatal(err)
	}
	s.CreateRequest(ctx, NewRequest{UserID: u.ID, Address: addr, Lines: []swag.Line{{ItemID: it.ID, Quantity: 1}}, Status: swag.StatusPending},
		func(prior map[int64]int) error {
			if prior[it.ID] != 0 {
				t.Errorf("rejected request counted: prior=%v", prior)
			}
			return nil
		})
}

func TestTransitionRequest(t *testing.T) {
	s := newStore(t)
	u := mkUser(t, s, "ident!1", "a@x.com")
	it := mkItem(t, s, "Sti/A", true)
	r, _ := s.CreateRequest(ctx, NewRequest{UserID: u.ID, Address: addr, Lines: []swag.Line{{ItemID: it.ID, Quantity: 1}}, Status: swag.StatusAwaitingPayment, ShippingFeeCents: 1500}, noCheck)

	if _, err := s.TransitionRequest(ctx, r.ID, swag.StatusDispatched, ""); !errors.Is(err, ErrBadTransition) {
		t.Fatalf("awaiting_payment -> dispatched err = %v", err)
	}
	got, err := s.MarkPaidByStripe(ctx, r.ID, "pi_1")
	if err != nil || got.Status != swag.StatusPending || got.PaidAt == nil {
		t.Fatalf("MarkPaidByStripe = %+v, %v", got, err)
	}
	got, err = s.MarkDispatched(ctx, r.ID, "pkg!1", ShipChanges{})
	if err != nil || got.Status != swag.StatusDispatched || got.TheseusOrderID != "pkg!1" {
		t.Fatalf("MarkDispatched = %+v, %v", got, err)
	}
	if _, err := s.MarkDispatched(ctx, r.ID, "pkg!2", ShipChanges{}); !errors.Is(err, ErrBadTransition) {
		t.Fatalf("double dispatch err = %v", err)
	}
	got, _ = s.SetTracking(ctx, r.ID, "9400", "USPS")
	if got.TrackingNumber != "9400" || got.Carrier != "USPS" {
		t.Fatalf("tracking = %+v", got)
	}
	all, _ := s.ListRequests(ctx, "")
	disp, _ := s.ListRequests(ctx, swag.StatusDispatched)
	pend, _ := s.ListRequests(ctx, swag.StatusPending)
	if len(all) != 1 || len(disp) != 1 || len(pend) != 0 {
		t.Fatalf("all=%d dispatched=%d pending=%d", len(all), len(disp), len(pend))
	}
	if all[0].UserEmail != "a@x.com" {
		t.Errorf("admin listing should include requester email, got %q", all[0].UserEmail)
	}
}

func TestSubmissions(t *testing.T) {
	s := newStore(t)
	a := mkUser(t, s, "ident!1", "author@x.com")
	b := mkUser(t, s, "ident!2", "other@x.com")
	sub, err := s.CreateSubmission(ctx, Submission{UserID: a.ID, Program: "Sprig", Title: "Sprig sticker", Kind: "sticker", FileURL: "https://cdn.hackclub.com/x.png", Quantity: 500})
	if err != nil || sub.Status != "submitted" {
		t.Fatalf("CreateSubmission = %+v, %v", sub, err)
	}
	mine, _ := s.ListSubmissionsByUser(ctx, a.ID)
	theirs, _ := s.ListSubmissionsByUser(ctx, b.ID)
	if len(mine) != 1 || len(theirs) != 0 {
		t.Fatalf("mine=%d theirs=%d", len(mine), len(theirs))
	}
	upd, err := s.UpdateSubmission(ctx, sub.ID, "stocked", "printed 500", "Sti/Spr/1")
	if err != nil || upd.Status != "stocked" || upd.SKU != "Sti/Spr/1" || upd.AdminNote != "printed 500" {
		t.Fatalf("UpdateSubmission = %+v, %v", upd, err)
	}
	if _, err := s.UpdateSubmission(ctx, sub.ID, "bogus", "", ""); err == nil {
		t.Fatal("invalid status should fail")
	}
	all, _ := s.ListSubmissions(ctx)
	if len(all) != 1 || all[0].UserEmail != "author@x.com" {
		t.Fatalf("all = %+v", all)
	}
}

func noCheck(map[int64]int) error { return nil }

func TestUpsertUserKeepsHCAAddress(t *testing.T) {
	s := newStore(t)
	home := swag.Address{FirstName: "O", LastName: "D", Line1: "15 Falls Rd", City: "Shelburne", State: "VT", PostalCode: "05482", Country: "US", Phone: "+18025550199"}
	u, err := s.UpsertUser(ctx, auth.Identity{ID: "ident!1", Email: "a@x.com", Address: &home})
	if err != nil {
		t.Fatal(err)
	}
	if u.Address == nil || *u.Address != home {
		t.Fatalf("address = %+v", u.Address)
	}
	// A later login without the address scope must not wipe it.
	u, _ = s.UpsertUser(ctx, auth.Identity{ID: "ident!1", Email: "a@x.com"})
	if u.Address == nil || u.Address.Line1 != "15 Falls Rd" {
		t.Fatalf("address lost: %+v", u.Address)
	}
	tok, _ := s.CreateSession(ctx, u.ID, time.Hour)
	bySession, _ := s.UserBySession(ctx, tok)
	if bySession.Address == nil || bySession.Address.City != "Shelburne" {
		t.Fatalf("session user address = %+v", bySession.Address)
	}
}

func TestStripePayment(t *testing.T) {
	s := newStore(t)
	u := mkUser(t, s, "ident!1", "a@x.com")
	it := mkItem(t, s, "Sti/A", true)
	r, _ := s.CreateRequest(ctx, NewRequest{UserID: u.ID, Address: addr, Lines: []swag.Line{{ItemID: it.ID, Quantity: 1}}, Status: swag.StatusAwaitingPayment, ShippingFeeCents: 1500}, noCheck)

	if err := s.SetStripePayment(ctx, r.ID, "pi_1"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetRequest(ctx, r.ID)
	if got.StripePaymentIntent != "pi_1" || got.Status != swag.StatusAwaitingPayment || got.PaidAt != nil {
		t.Fatalf("after starting payment = %+v", got)
	}
	got, err := s.MarkPaidByStripe(ctx, r.ID, "pi_1")
	if err != nil || got.Status != swag.StatusPending || got.PaidAt == nil || got.StripePaymentIntent != "pi_1" {
		t.Fatalf("MarkPaidByStripe = %+v, %v", got, err)
	}
	if _, err := s.MarkPaidByStripe(ctx, r.ID, "pi_1"); !errors.Is(err, ErrBadTransition) {
		t.Fatalf("paying twice err = %v", err)
	}
	if err := s.SetStripePayment(ctx, 99999, "pi_2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing request err = %v", err)
	}
}

// An admin can change quantities when shipping: what was asked for is kept,
// limits count what shipped, and a removed line ships none.
func TestDispatchWithChanges(t *testing.T) {
	s := newStore(t)
	u := mkUser(t, s, "ident!1", "a@x.com")
	a, b, c := mkItem(t, s, "Sti/A", true), mkItem(t, s, "Sti/B", true), mkItem(t, s, "Sti/C", true)
	r, _ := s.CreateRequest(ctx, NewRequest{UserID: u.ID, Address: addr, Status: swag.StatusPending,
		Lines: []swag.Line{{ItemID: a.ID, Quantity: 3}, {ItemID: b.ID, Quantity: 2}, {ItemID: c.ID, Quantity: 1}}}, noCheck)

	got, err := s.MarkDispatched(ctx, r.ID, "pkg!1", ShipChanges{Quantities: map[string]int{"Sti/A": 1, "Sti/B": 0, "Sti/C": 1}, Note: "Out of B."})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][2]int{"Sti/A": {1, 3}, "Sti/B": {0, 2}, "Sti/C": {1, 0}} // quantity, requested (0 = unchanged)
	for _, l := range got.Lines {
		req := 0
		if l.RequestedQuantity != nil {
			req = *l.RequestedQuantity
		}
		if w := want[l.SKU]; l.Quantity != w[0] || req != w[1] {
			t.Errorf("%s: quantity %d requested %d, want %v", l.SKU, l.Quantity, req, w)
		}
	}
	if got.AdminNote != "Out of B." || got.Status != swag.StatusDispatched {
		t.Fatalf("request = %+v", got)
	}
	s.CreateRequest(ctx, NewRequest{UserID: u.ID, Address: addr, Lines: []swag.Line{{ItemID: a.ID, Quantity: 1}}, Status: swag.StatusPending},
		func(prior map[int64]int) error {
			if prior[a.ID] != 1 || prior[b.ID] != 0 {
				t.Errorf("prior = %v, want what shipped", prior)
			}
			return nil
		})
}

func TestCloseRequestRecordsRefund(t *testing.T) {
	s := newStore(t)
	u := mkUser(t, s, "ident!1", "a@x.com")
	it := mkItem(t, s, "Sti/A", true)
	r, _ := s.CreateRequest(ctx, NewRequest{UserID: u.ID, Address: addr, Lines: []swag.Line{{ItemID: it.ID, Quantity: 1}}, Status: swag.StatusPending, ShippingFeeCents: 1200}, noCheck)
	got, err := s.CloseRequest(ctx, r.ID, swag.StatusRejected, "Out of stock.", true)
	if err != nil || got.Status != swag.StatusRejected || got.AdminNote != "Out of stock." || got.RefundedAt == nil || got.RefundedCents != 1200 {
		t.Fatalf("CloseRequest = %+v, %v", got, err)
	}
	r2, _ := s.CreateRequest(ctx, NewRequest{UserID: u.ID, Address: addr, Lines: []swag.Line{{ItemID: it.ID, Quantity: 1}}, Status: swag.StatusPending}, noCheck)
	if got, _ := s.CloseRequest(ctx, r2.ID, swag.StatusCancelled, "", false); got.RefundedAt != nil {
		t.Fatalf("unrefunded = %+v", got)
	}
	if got, err := s.RecordRefund(ctx, r2.ID, 300); err != nil || got.RefundedCents != 300 || got.RefundedAt == nil {
		t.Fatalf("RecordRefund = %+v, %v", got, err)
	}
}

func TestDispatchTimeAndWarehouseCosts(t *testing.T) {
	s := newStore(t)
	u := mkUser(t, s, "ident!1", "a@x.com")
	it := mkItem(t, s, "Sti/A", true)
	r, _ := s.CreateRequest(ctx, NewRequest{UserID: u.ID, Address: addr, Lines: []swag.Line{{ItemID: it.ID, Quantity: 1}}, Status: swag.StatusPending}, noCheck)
	if r.DispatchedAt != nil || r.WarehouseCosts != (theseus.Costs{}) {
		t.Fatalf("new request = %+v", r)
	}
	got, err := s.MarkDispatched(ctx, r.ID, "pkg!1", ShipChanges{})
	if err != nil || got.DispatchedAt == nil || time.Since(*got.DispatchedAt) > time.Minute {
		t.Fatalf("dispatched = %+v, %v", got, err)
	}

	// The warehouse charges contents and labor when it takes the order, and postage once it's mailed.
	labor, contents, postage := 300, 1215, 471
	got, err = s.SetWarehouseCosts(ctx, r.ID, theseus.Costs{ContentsCents: &contents, LaborCents: &labor}, nil)
	if err != nil || *got.WarehouseCosts.LaborCents != 300 || got.WarehouseCosts.PostageCents != nil || got.MailedAt != nil {
		t.Fatalf("before mailing = %+v, %v", got.WarehouseCosts, err)
	}
	mailed := time.Date(2026, 10, 17, 20, 0, 0, 0, time.UTC)
	got, _ = s.SetWarehouseCosts(ctx, r.ID, theseus.Costs{ContentsCents: &contents, LaborCents: &labor, PostageCents: &postage}, &mailed)
	if *got.WarehouseCosts.PostageCents != 471 || !got.MailedAt.Equal(mailed) {
		t.Fatalf("mailed = %+v %v", got.WarehouseCosts, got.MailedAt)
	}
	// A later refresh that's missing a figure keeps the one it had.
	got, _ = s.SetWarehouseCosts(ctx, r.ID, theseus.Costs{}, nil)
	if got.WarehouseCosts.PostageCents == nil || got.MailedAt == nil {
		t.Fatalf("kept = %+v %v", got.WarehouseCosts, got.MailedAt)
	}
	if _, err := s.SetWarehouseCosts(ctx, 9999, theseus.Costs{}, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing request err = %v", err)
	}

	pending, _ := s.CreateRequest(ctx, NewRequest{UserID: u.ID, Address: addr, Lines: []swag.Line{{ItemID: it.ID, Quantity: 1}}, Status: swag.StatusPending}, noCheck)
	since, _ := s.ListDispatchedSince(ctx, time.Now().Add(-time.Hour))
	later, _ := s.ListDispatchedSince(ctx, time.Now().Add(time.Hour))
	if len(since) != 1 || since[0].ID != r.ID || since[0].ID == pending.ID || len(since[0].Lines) != 1 || len(later) != 0 {
		t.Fatalf("since = %+v, later = %+v", since, later)
	}
}

// Open requests hold their SKUs until they ship; closed and shipped ones don't.
func TestHeldQuantities(t *testing.T) {
	s := newStore(t)
	u := mkUser(t, s, "ident!1", "a@x.com")
	shirt, _ := s.CreateItem(ctx, swag.Item{SKU: "Swa/Shirt/HC", Name: "T-shirt", Visible: true, MaxPerRequest: 9, Variants: shirtSizes, AllowBackorder: true})
	if !shirt.AllowBackorder {
		t.Fatalf("allow_backorder not saved: %+v", shirt)
	}
	pin := mkItem(t, s, "Swa/Pin", true)
	mk := func(status swag.Status, lines ...swag.Line) *Request {
		r, err := s.CreateRequest(ctx, NewRequest{UserID: u.ID, Address: addr, Status: status, Lines: lines}, noCheck)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	mk(swag.StatusPending, swag.Line{ItemID: pin.ID, Quantity: 2}, swag.Line{ItemID: shirt.ID, Quantity: 1, SKU: "Swa/Shirt/HC/M"})
	mk(swag.StatusAwaitingPayment, swag.Line{ItemID: pin.ID, Quantity: 3})
	closed := mk(swag.StatusPending, swag.Line{ItemID: pin.ID, Quantity: 50})
	if _, err := s.CloseRequest(ctx, closed.ID, swag.StatusRejected, "", false); err != nil {
		t.Fatal(err)
	}
	// Dispatched but not yet mailed: the warehouse still has it on its shelf.
	ship := func(lines ...swag.Line) *Request {
		r := mk(swag.StatusPending, lines...)
		r, err := s.MarkDispatched(ctx, r.ID, "ord_"+strconv.FormatInt(r.ID, 10), ShipChanges{})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	ship(swag.Line{ItemID: pin.ID, Quantity: 4})
	tracked := ship(swag.Line{ItemID: pin.ID, Quantity: 100})
	if _, err := s.SetTracking(ctx, tracked.ID, "9400", "usps"); err != nil {
		t.Fatal(err)
	}
	mailed := ship(swag.Line{ItemID: pin.ID, Quantity: 200})
	now := time.Now()
	if _, err := s.SetWarehouseCosts(ctx, mailed.ID, theseus.Costs{}, &now); err != nil {
		t.Fatal(err)
	}
	imported := ship(swag.Line{ItemID: pin.ID, Quantity: 400})
	if _, err := s.pool.Exec(ctx, `UPDATE swag_requests SET source = 'fillout' WHERE id = $1`, imported.ID); err != nil {
		t.Fatal(err)
	}
	held, err := s.HeldQuantities(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(held) != 2 || held["Swa/Pin"] != 9 || held["Swa/Shirt/HC/M"] != 1 {
		t.Fatalf("held = %v", held)
	}
}

// Lowering what a dispatched, unmailed request ships keeps what was asked for
// and adds the note to the requester's note.
func TestReduceDispatchedLines(t *testing.T) {
	s := newStore(t)
	u := mkUser(t, s, "ident!1", "a@x.com")
	pin := mkItem(t, s, "Swa/Pin", true)
	card := mkItem(t, s, "Pri/Card", true)
	r, err := s.CreateRequest(ctx, NewRequest{UserID: u.ID, Address: addr, Status: swag.StatusPending,
		Lines: []swag.Line{{ItemID: pin.ID, Quantity: 5}, {ItemID: card.ID, Quantity: 10}}}, noCheck)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReduceDispatchedLines(ctx, r.ID, map[string]int{"Swa/Pin": 1}, "x"); !errors.Is(err, ErrBadTransition) {
		t.Fatalf("pending request changed: %v", err)
	}
	if _, err := s.MarkDispatched(ctx, r.ID, "ord_1", ShipChanges{Note: "Thanks!"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReduceDispatchedLines(ctx, r.ID, map[string]int{"Swa/Pin": 1}, "Pins ran out.")
	if err != nil {
		t.Fatal(err)
	}
	if got.AdminNote != "Thanks!\n\nPins ran out." {
		t.Fatalf("note = %q", got.AdminNote)
	}
	for _, l := range got.Lines {
		switch l.SKU {
		case "Swa/Pin":
			if l.Quantity != 1 || l.RequestedQuantity == nil || *l.RequestedQuantity != 5 {
				t.Fatalf("pin line = %+v", l)
			}
		case "Pri/Card":
			if l.Quantity != 10 || l.RequestedQuantity != nil {
				t.Fatalf("card line = %+v", l)
			}
		}
	}
	// A second cut keeps the original ask.
	got, err = s.ReduceDispatchedLines(ctx, r.ID, map[string]int{"Swa/Pin": 0}, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range got.Lines {
		if l.SKU == "Swa/Pin" && (l.Quantity != 0 || *l.RequestedQuantity != 5) {
			t.Fatalf("after second cut: %+v", l)
		}
	}
	if got.AdminNote != "Thanks!\n\nPins ran out." {
		t.Fatalf("empty note changed the note: %q", got.AdminNote)
	}
	now := time.Now()
	if _, err := s.SetWarehouseCosts(ctx, r.ID, theseus.Costs{}, &now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReduceDispatchedLines(ctx, r.ID, map[string]int{"Pri/Card": 1}, ""); !errors.Is(err, ErrAlreadyMailed) {
		t.Fatalf("mailed request changed: %v", err)
	}
}
