package store

import (
	"testing"

	"github.com/hackclub/merch/internal/swag"
)

func TestSentEmails(t *testing.T) {
	s := newStore(t)
	u := mkUser(t, s, "ident!1", "a@x.com")
	r, err := s.CreateRequest(ctx, NewRequest{UserID: u.ID, Address: addr, Status: swag.StatusPending,
		Lines: []swag.Line{{ItemID: mkItem(t, s, "Sti/A", true).ID, Quantity: 1}}}, noCheck)
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.LogEmail(ctx, SentEmail{Kind: "received", RequestID: &r.ID, UserID: &u.ID, To: "a@x.com",
		Subject: "We got it", Preview: "p", Body: "Hi,\n\nThanks!", IdempotencyKey: "k1", Status: "sent", Release: "rel"})
	if err != nil || first.ID == 0 || first.At.IsZero() {
		t.Fatalf("LogEmail = %+v %v", first, err)
	}
	if _, err := s.LogEmail(ctx, SentEmail{Kind: "rejected", To: "b@x.com", Subject: "No", Body: "Sorry", IdempotencyKey: "k2", Status: "failed", Error: "loops: 500"}); err != nil {
		t.Fatal(err)
	}

	all, err := s.ListEmails(ctx, EmailFilter{Limit: 10})
	if err != nil || len(all) != 2 || all[0].Kind != "rejected" || all[0].Error != "loops: 500" || all[1].Body != "Hi,\n\nThanks!" {
		t.Fatalf("ListEmails = %+v %v", all, err)
	}
	mine, err := s.ListEmails(ctx, EmailFilter{RequestID: r.ID, Limit: 10})
	if err != nil || len(mine) != 1 || mine[0].ID != first.ID || *mine[0].RequestID != r.ID || mine[0].Release != "rel" {
		t.Fatalf("ListEmails(request) = %+v %v", mine, err)
	}
	failed, err := s.ListEmails(ctx, EmailFilter{Status: "failed", Limit: 10})
	if err != nil || len(failed) != 1 || failed[0].To != "b@x.com" {
		t.Fatalf("ListEmails(failed) = %+v %v", failed, err)
	}
	older, err := s.ListEmails(ctx, EmailFilter{Before: all[0].ID, Limit: 10})
	if err != nil || len(older) != 1 || older[0].ID != first.ID {
		t.Fatalf("ListEmails(before) = %+v %v", older, err)
	}
}
