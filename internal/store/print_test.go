package store

import (
	"testing"
	"time"

	"github.com/hackclub/merch/internal/swag"
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

	// Once stocked, the first shipment is a swag request; it's recorded once.
	it := mkItem(t, s, "Pri/Hav/4x6/1st", false)
	req, err := s.CreateRequest(ctx, NewRequest{UserID: a.ID, Address: addr, Purpose: "staff", Status: "pending",
		Lines: []swag.Line{{ItemID: it.ID, Quantity: 25}}}, func(map[int64]int) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.SetPrintShipment(ctx, pr.ID, req.ID); err != nil || got.ShipmentRequestID == nil || *got.ShipmentRequestID != req.ID {
		t.Fatalf("SetPrintShipment = %+v, %v", got, err)
	}
	if _, err := s.SetPrintShipment(ctx, pr.ID, req.ID); err != ErrNotFound {
		t.Fatalf("second SetPrintShipment = %v, want ErrNotFound", err)
	}
}
