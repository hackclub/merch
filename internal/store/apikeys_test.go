package store

import (
	"errors"
	"strings"
	"testing"

	"github.com/hackclub/merch/internal/swag"
)

func TestAPIKeys(t *testing.T) {
	s := newStore(t)
	admin := mkUser(t, s, "ident!admin", "orpheus@hackclub.com")
	k, tok, err := s.CreateAPIKey(ctx, "Review agent", admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(tok, "merch_") || len(tok) < 40 || !strings.HasPrefix(tok, k.Prefix) || k.Name != "Review agent" {
		t.Fatalf("key %+v token %q", k, tok)
	}
	got, owner, err := s.APIKeyByToken(ctx, tok)
	if err != nil || got.ID != k.ID || owner.ID != admin.ID {
		t.Fatalf("APIKeyByToken = %+v %+v %v", got, owner, err)
	}
	if _, _, err := s.APIKeyByToken(ctx, tok+"x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("wrong token: %v", err)
	}
	keys, err := s.ListAPIKeys(ctx)
	if err != nil || len(keys) != 1 || keys[0].LastUsedAt == nil || keys[0].CreatedByEmail != admin.Email {
		t.Fatalf("ListAPIKeys = %+v %v", keys, err)
	}
	if k, err = s.RevokeAPIKey(ctx, k.ID); err != nil || k.RevokedAt == nil {
		t.Fatalf("revoke = %+v %v", k, err)
	}
	if _, _, err := s.APIKeyByToken(ctx, tok); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked key still works: %v", err)
	}
	if _, err := s.RevokeAPIKey(ctx, k.ID+100); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoke missing key: %v", err)
	}
}

func TestRequesterAndSameAddress(t *testing.T) {
	s := newStore(t)
	it := mkItem(t, s, "Sti/A", true)
	a := mkUser(t, s, "ident!a", "a@x.com")
	b := mkUser(t, s, "ident!b", "b@x.com")
	c := mkUser(t, s, "ident!c", "c@x.com")
	mk := func(u *User, ad swag.Address) *Request {
		t.Helper()
		r, err := s.CreateRequest(ctx, NewRequest{UserID: u.ID, Address: ad, Purpose: swag.PurposePersonal, Status: swag.StatusPending,
			Lines: []swag.Line{{ItemID: it.ID, Quantity: 1}}}, func(map[int64]int) error { return nil })
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	ra := mk(a, addr)
	// The same place written differently, by someone else.
	same := addr
	same.Line1, same.PostalCode = "1  MAIN.", "05401 "
	rb := mk(b, same)
	other := addr
	other.Line1 = "2 Main"
	mk(c, other)

	got, err := s.RequestsToAddress(ctx, ra.Address, a.ID)
	if err != nil || len(got) != 1 || got[0].ID != rb.ID {
		t.Fatalf("RequestsToAddress = %+v %v", got, err)
	}
	p, err := s.GetRequester(ctx, a.ID)
	if err != nil || p.Email != "a@x.com" || p.CreatedAt.IsZero() || p.Imported {
		t.Fatalf("GetRequester = %+v %v", p, err)
	}

	r, err := s.AppendInternalNote(ctx, ra.ID, "first")
	if err != nil || r.InternalNote != "first" {
		t.Fatalf("append = %q %v", r.InternalNote, err)
	}
	if r, _ = s.AppendInternalNote(ctx, ra.ID, "second"); r.InternalNote != "first\n\nsecond" {
		t.Fatalf("append again = %q", r.InternalNote)
	}
}
