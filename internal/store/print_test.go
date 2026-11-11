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
		UserID: a.ID, Product: "postcard_4x6", Title: "Haven postcard", Finish: "linen", Program: "Haven", Deadline: "Haven in Boston, Nov 14-16", Quantity: 1000,
		FrontURL: "https://cdn.hackclub.com/front.png", BackURL: "https://cdn.hackclub.com/back.png",
		Note: "For Haven events", NeededBy: &due, EstimateCents: 11958,
		InitialQuantity: 25, Address: &addr, FrontPreviewURL: "https://cdn.hackclub.com/front.jpg",
	})
	if err != nil || pr.ID == 0 || pr.Status != "submitted" || pr.UserEmail != "a@x.com" || pr.NeededBy == nil || !pr.NeededBy.Equal(due) {
		t.Fatalf("CreatePrintRequest = %+v, %v", pr, err)
	}
	if pr.Finish != "linen" || pr.Program != "Haven" || pr.Deadline != "Haven in Boston, Nov 14-16" {
		t.Fatalf("program %q, deadline %q", pr.Program, pr.Deadline)
	}
	if pr.InitialQuantity != 25 || pr.Address == nil || pr.Address.City != "Burlington" || pr.FrontPreviewURL == "" || pr.ShipmentRequestID != nil {
		t.Fatalf("first shipment = %d to %+v, preview %q, request %v", pr.InitialQuantity, pr.Address, pr.FrontPreviewURL, pr.ShipmentRequestID)
	}
	mine, _ := s.ListPrintRequestsByUser(ctx, a.ID)
	theirs, _ := s.ListPrintRequestsByUser(ctx, b.ID)
	if len(mine) != 1 || len(theirs) != 0 {
		t.Fatalf("mine = %d, theirs = %d", len(mine), len(theirs))
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
