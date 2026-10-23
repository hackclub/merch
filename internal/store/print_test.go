package store

import (
	"testing"
	"time"
)

func TestPrintRequests(t *testing.T) {
	s := newStore(t)
	a, b := mkUser(t, s, "a", "a@x.com"), mkUser(t, s, "b", "b@x.com")
	due := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	pr, err := s.CreatePrintRequest(ctx, PrintRequest{
		UserID: a.ID, Product: "postcard_4x6", Title: "Haven postcard", Quantity: 1000,
		FrontURL: "https://cdn.hackclub.com/front.png", BackURL: "https://cdn.hackclub.com/back.png",
		Note: "For Haven events", NeededBy: &due, EstimateCents: 11958,
	})
	if err != nil || pr.ID == 0 || pr.Status != "submitted" || pr.UserEmail != "a@x.com" || pr.NeededBy == nil || !pr.NeededBy.Equal(due) {
		t.Fatalf("CreatePrintRequest = %+v, %v", pr, err)
	}
	mine, _ := s.ListPrintRequestsByUser(ctx, a.ID)
	theirs, _ := s.ListPrintRequestsByUser(ctx, b.ID)
	if len(mine) != 1 || len(theirs) != 0 {
		t.Fatalf("mine = %d, theirs = %d", len(mine), len(theirs))
	}
	upd, err := s.UpdatePrintRequest(ctx, pr.ID, "stocked", "In the shop now", "Pri/Hav/4x6/1st")
	if err != nil || upd.Status != "stocked" || upd.SKU != "Pri/Hav/4x6/1st" || upd.AdminNote != "In the shop now" {
		t.Fatalf("UpdatePrintRequest = %+v, %v", upd, err)
	}
	if _, err := s.UpdatePrintRequest(ctx, pr.ID, "bogus", "", ""); err == nil {
		t.Fatal("bogus status saved")
	}
	if _, err := s.UpdatePrintRequest(ctx, 999, "ordered", "", ""); err != ErrNotFound {
		t.Fatalf("missing = %v", err)
	}
	all, _ := s.ListPrintRequests(ctx)
	if len(all) != 1 || all[0].Title != "Haven postcard" {
		t.Fatalf("all = %+v", all)
	}
	got, err := s.GetPrintRequest(ctx, pr.ID)
	if err != nil || got.Quantity != 1000 {
		t.Fatalf("GetPrintRequest = %+v, %v", got, err)
	}
}
