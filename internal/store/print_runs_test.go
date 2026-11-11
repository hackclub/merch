package store

import (
	"errors"
	"github.com/hackclub/merch/internal/swag"
	"sync"
	"testing"
	"time"
)

func TestPrintRunReceiptAndAtomicRelease(t *testing.T) {
	for _, country := range []string{"US", "DE"} {
		t.Run(country, func(t *testing.T) {
			s := newStore(t)
			u := mkUser(t, s, "release", "release@x.com")
			ad := addr
			ad.Country = country
			ad.Phone = "+12025550123"
			_, err := s.CreatePrintRequest(ctx, PrintRequest{UserID: u.ID, Product: "postcard_4x6", Title: "Demo cards", Quantity: 800, FrontURL: "https://example.com/art.png", InitialQuantity: 25, Address: &ad})
			if err != nil {
				t.Fatal(err)
			}
			runs, _ := s.PrintRuns(ctx, 0)
			r := runs[0]
			approved := time.Now().Add(-30 * time.Minute)
			r, err = s.ActPrintRun(ctx, r.ID, PrintAction{Version: r.Version, Action: "approve", ActorID: u.ID, ApprovedQuantity: 800, Specifications: "Linen, art and budget checked", Now: approved})
			if err != nil {
				t.Fatal(err)
			}
			it := mkItem(t, s, "Demo/Card", false)
			_, err = s.pool.Exec(ctx, `update items set images=ARRAY['https://example.com/art.jpg'] where id=$1`, it.ID)
			if err != nil {
				t.Fatal(err)
			}
			created := approved.Add(time.Minute)
			p := swag.PrintPOEvidence{PrintPOLink: swag.PrintPOLink{POID: 12, LineID: 34, SKU: it.SKU}, Quantity: 800, CreatedAt: &created, SyncedAt: time.Now(), PolledAt: time.Now()}
			r, err = s.ActPrintRun(ctx, r.ID, PrintAction{Version: r.Version, Action: "link", ActorID: u.ID, SKU: it.SKU, ItemID: it.ID, ConfirmationURL: "https://example.com/confirmation", Evidence: []swag.PrintPOEvidence{p}})
			if err != nil {
				t.Fatal(err)
			}
			r, err = s.ActPrintRun(ctx, r.ID, PrintAction{Version: r.Version, Action: "observe", Auto: true, Evidence: []swag.PrintPOEvidence{p}})
			if err != nil || r.Stage != swag.PrintOrdered {
				t.Fatalf("ordered %+v %v", r, err)
			}
			p.Completed = true
			p.CompletedAt = nil // Unknown completion date remains unknown.
			r, err = s.ActPrintRun(ctx, r.ID, PrintAction{Version: r.Version, Action: "observe", Auto: true, Evidence: []swag.PrintPOEvidence{p}})
			if err != nil || r.Stage != swag.PrintReceived {
				t.Fatalf("received %+v %v", r, err)
			}
			if got, _ := s.ItemsBySKU(ctx, []string{it.SKU}); got[0].Visible {
				t.Fatal("receipt published listing")
			}
			r, err = s.ActPrintRun(ctx, r.ID, PrintAction{Version: r.Version, Action: "observe", Auto: true, Evidence: []swag.PrintPOEvidence{p}})
			if err != nil {
				t.Fatal(err)
			}
			ev, _ := s.PrintRunHistory(ctx, r.ID)
			count := 0
			for _, e := range ev {
				if e.Stage == swag.PrintReceived {
					count++
				}
			}
			if count != 1 {
				t.Fatalf("duplicate receipt history %d", count)
			}
			state, fee := swag.StatusPending, 0
			if country == "DE" {
				state = swag.StatusAwaitingPayment
				fee = 1200
			}
			a := PrintAction{Version: r.Version, Action: "release", ActorID: u.ID, Sellable: 800, ReleaseStatus: state, ReleaseFee: fee}
			var wg sync.WaitGroup
			out := make(chan *PrintRun, 2)
			errs := make(chan error, 2)
			for i := 0; i < 2; i++ {
				wg.Add(1)
				go func() { defer wg.Done(); p, e := s.ActPrintRun(ctx, r.ID, a); out <- p; errs <- e }()
			}
			wg.Wait()
			close(out)
			close(errs)
			for e := range errs {
				if e != nil {
					t.Fatal(e)
				}
			}
			var shipmentID int64
			for p := range out {
				if p.ShipmentRequestID == nil {
					t.Fatal("missing first shipment")
				}
				if shipmentID != 0 && shipmentID != *p.ShipmentRequestID {
					t.Fatal("duplicate parcels")
				}
				shipmentID = *p.ShipmentRequestID
			}
			parcel, err := s.GetRequest(ctx, shipmentID)
			if err != nil || parcel.Status != state || parcel.ShippingFeeCents != fee {
				t.Fatalf("parcel %+v %v", parcel, err)
			}
			held, _ := s.HeldQuantities(ctx)
			if held[it.SKU] != 25 {
				t.Fatalf("holds %+v", held)
			}
			r, _ = s.PrintRun(ctx, r.ID)
			p.Completed = false
			r, err = s.ActPrintRun(ctx, r.ID, PrintAction{Version: r.Version, Action: "observe", Auto: true, Evidence: []swag.PrintPOEvidence{p}})
			if err != nil || r.Stage != swag.PrintAvailable || !r.Reconcile {
				t.Fatalf("reopened %+v %v", r, err)
			}
			next, err := s.ReorderPrintRun(ctx, r.ID, r.Version)
			if err != nil {
				t.Fatal(err)
			}
			again, err := s.ReorderPrintRun(ctx, r.ID, r.Version)
			if err != nil || next.ID != again.ID {
				t.Fatalf("reorder retry %+v %v", again, err)
			}
		})
	}
}

func TestCanonicalPrintRunApprovalAndConcurrency(t *testing.T) {
	s := newStore(t)
	u := mkUser(t, s, "print-owner", "owner@x.com")
	p, err := s.CreatePrintRequest(ctx, PrintRequest{UserID: u.ID, Product: "postcard_4x6", Title: "Demo cards", Quantity: 800, FrontURL: "https://example.com/front.png", InitialQuantity: 25, Address: &addr})
	if err != nil {
		t.Fatal(err)
	}
	runs, err := s.PrintRuns(ctx, u.ID)
	if err != nil || len(runs) != 1 {
		t.Fatalf("runs %v %v", runs, err)
	}
	r := runs[0]
	if r.SourcePrintID == nil || *r.SourcePrintID != p.ID || r.Origin != "staff" || r.Stage != swag.PrintSubmitted {
		t.Fatalf("run %+v", r)
	}
	ev, _ := s.PrintRunHistory(ctx, r.ID)
	if len(ev) != 1 || ev[0].Stage != swag.PrintSubmitted {
		t.Fatalf("history %+v", ev)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.ActPrintRun(ctx, r.ID, PrintAction{Version: r.Version, Action: "approve", ActorID: u.ID, ApprovedQuantity: 700, Specifications: "Linen, 4x6, both sides; budget checked"})
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	success, conflict := 0, 0
	for err := range errs {
		if err == nil {
			success++
		} else if errors.Is(err, ErrPrintConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("success %d conflict %d", success, conflict)
	}
	r, _ = s.PrintRun(ctx, r.ID)
	if r.Request.Quantity != 800 || r.ApprovedQuantity != 700 {
		t.Fatalf("quantities %+v", r)
	}
	ev, _ = s.PrintRunHistory(ctx, r.ID)
	if len(ev) != 2 {
		t.Fatalf("events %+v", ev)
	}
	if _, err = s.ActPrintRun(ctx, r.ID, PrintAction{Version: r.Version, Action: "receive", ActorID: u.ID}); err == nil {
		t.Fatal("manual receipt accepted")
	}
	var count int
	s.pool.QueryRow(ctx, `select count(*) from print_notifications where run_id=$1`, r.ID).Scan(&count)
	if count != 2 {
		t.Fatalf("jobs %d", count)
	}
}

func TestCanonicalAuthorAndRetry(t *testing.T) {
	s := newStore(t)
	u := mkUser(t, s, "author-run", "author@x.com")
	_, err := s.CreateSubmission(ctx, Submission{UserID: u.ID, Program: "Demo", Title: "Demo stickers", Kind: "sticker", Quantity: 500, FileURL: "https://example.com/art.png"})
	if err != nil {
		t.Fatal(err)
	}
	runs, _ := s.PrintRuns(ctx, 0)
	if len(runs) != 1 || runs[0].Origin != "author" {
		t.Fatalf("runs %+v", runs)
	}
	calls := 0
	deliver := func(PrintNotification) error {
		calls++
		if calls == 1 {
			return errors.New("temporary failure")
		}
		return nil
	}
	if err = s.DeliverPrintNotifications(ctx, "email", deliver); err != nil {
		t.Fatal(err)
	}
	var state string
	s.pool.QueryRow(ctx, `select status from print_notifications limit 1`).Scan(&state)
	if state != "failed" {
		t.Fatalf("state %s", state)
	}
	s.pool.Exec(ctx, `update print_notifications set next_attempt_at=now()`)
	s.DeliverPrintNotifications(ctx, "email", deliver)
	s.DeliverPrintNotifications(ctx, "email", deliver)
	if calls != 2 {
		t.Fatalf("delivery calls %d", calls)
	}
}
