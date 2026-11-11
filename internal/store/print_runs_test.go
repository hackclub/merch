package store

import (
	"errors"
	"github.com/hackclub/merch/internal/swag"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLegacyPrintMigrationPreservesLinksAndUnknownDates(t *testing.T) {
	s := newStore(t)
	u := mkUser(t, s, "legacy-demo", "legacy@x.com")
	migration, err := os.ReadFile("../db/migrations/00037_print_runs.sql")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(string(migration), "-- +goose Down")
	if _, err = s.pool.Exec(ctx, parts[1]); err != nil {
		t.Fatal(err)
	}
	due := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	p := &PrintRequest{}
	err = s.pool.QueryRow(ctx, `insert into print_requests(user_id,product,title,quantity,front_url,needed_by,initial_quantity,address) values($1,'postcard_4x6','Legacy demo',800,'https://example.com/art.png',$2,25,$3) returning id`, u.ID, due, addr).Scan(&p.ID)
	if err != nil {
		t.Fatal(err)
	}
	it := mkItem(t, s, "Demo/Legacy", true)
	parcel := &Request{}
	err = s.pool.QueryRow(ctx, `insert into swag_requests(user_id,status,purpose,first_name,last_name,line_1,city,state,postal_code,country) values($1,'pending','staff','Demo','Person','123 Example Lane','Exampleville','VT','05401','US') returning id`, u.ID).Scan(&parcel.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.pool.Exec(ctx, `update print_requests set shipment_request_id=$2,status='stocked',admin_note='Original requester update',sku=$3 where id=$1`, p.ID, parcel.ID, it.SKU)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.pool.Exec(ctx, `insert into author_submissions(user_id,program,title,kind,quantity,file_url,status,admin_note) values($1,'Demo','Custom demo','sticker',500,'https://example.com/sticker.png','printing','Printer order not documented')`, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.pool.Exec(ctx, parts[0]); err != nil {
		t.Fatal(err)
	}
	runs, err := s.PrintRuns(ctx, u.ID)
	if err != nil || len(runs) != 2 {
		t.Fatalf("runs %+v %v", runs, err)
	}
	for _, r := range runs {
		if r.Origin != "legacy" || !r.LegacyReview {
			t.Fatalf("provenance %+v", r)
		}
		if r.SourcePrintID != nil {
			if r.Stage != swag.PrintReceived || r.ShipmentRequestID == nil || *r.ShipmentRequestID != parcel.ID || r.Request.NeededBy == nil || !r.Request.NeededBy.Equal(due) || r.PublicNote != "Original requester update" {
				t.Fatalf("legacy print %+v", r)
			}
			events, _ := s.PrintRunHistory(ctx, r.ID)
			for _, e := range events {
				if e.Action == "legacy_import" && e.At != nil {
					t.Fatal("invented stocked date")
				}
			}
		} else {
			if r.Stage != swag.PrintApproved {
				t.Fatal("printing should retain approval and require audit, without inferring an order")
			}
			if _, err := s.ActPrintRun(ctx, r.ID, PrintAction{Version: r.Version, Action: "approve", ActorID: u.ID, ApprovedQuantity: 500, Specifications: "Checked"}); err == nil {
				t.Fatal("replayed approval for a legacy printing run")
			}
			if _, err := s.ActPrintRun(ctx, r.ID, PrintAction{Version: r.Version, Action: "link", ActorID: u.ID, SKU: "Demo/Legacy", ConfirmationURL: "https://example.com/confirmation", Evidence: []swag.PrintPOEvidence{{PrintPOLink: swag.PrintPOLink{POID: 42, LineID: 10, SKU: "Demo/Legacy"}}}}); !errors.Is(err, ErrPrintAction) {
				t.Fatalf("legacy link must require audit, got %v", err)
			}
		}
	}
	var jobs, parcels int
	s.pool.QueryRow(ctx, `select count(*) from print_notifications`).Scan(&jobs)
	s.pool.QueryRow(ctx, `select count(*) from swag_requests`).Scan(&parcels)
	if jobs != 0 || parcels != 1 {
		t.Fatalf("replayed jobs=%d parcels=%d", jobs, parcels)
	}
}

func TestPrintNotificationCrashWindow(t *testing.T) {
	s := newStore(t)
	u := mkUser(t, s, "notice", "notice@x.com")
	s.CreateSubmission(ctx, Submission{UserID: u.ID, Program: "Demo", Title: "Demo", Kind: "sticker", Quantity: 50, FileURL: "https://example.com/art.png"})
	s.pool.Exec(ctx, `update print_notifications set status='sending',first_attempt_at=now()-interval '24 hours',next_attempt_at=now()-interval '1 minute',attempts=1`)
	called := false
	if err := s.DeliverPrintNotifications(ctx, "email", func(PrintNotification) error { called = true; return nil }); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("blind resend after transport deduplication expired")
	}
	var status string
	s.pool.QueryRow(ctx, `select status from print_notifications limit 1`).Scan(&status)
	if status != "uncertain" {
		t.Fatal(status)
	}
}

func TestUnconfiguredPrintSlackIsSkipped(t *testing.T) {
	s := newStore(t)
	u := mkUser(t, s, "skip-dm", "skip@x.com")
	if _, err := s.pool.Exec(ctx, `update users set slack_id='UDEMO' where id=$1`, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePrintRequest(ctx, PrintRequest{UserID: u.ID, Product: "postcard_4x6", Title: "Demo cards", Quantity: 800, FrontURL: "https://example.com/art.png", SlackOptIn: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.SkipUnconfiguredPrintSlack(ctx); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := s.pool.QueryRow(ctx, `select status from print_notifications where channel='slack'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "skipped" {
		t.Fatal(status)
	}
	if err := s.DeliverPrintNotifications(ctx, "slack", func(PrintNotification) error { t.Fatal("skipped DM delivered"); return nil }); err != nil {
		t.Fatal(err)
	}
}

func TestPrintSlackOptInAndPrivateNotes(t *testing.T) {
	s := newStore(t)
	u := mkUser(t, s, "slack-demo", "slack@x.com")
	s.pool.Exec(ctx, `update users set slack_id='UDEMO' where id=$1`, u.ID)
	_, err := s.CreatePrintRequest(ctx, PrintRequest{UserID: u.ID, Product: "postcard_4x6", Title: "Demo cards", Quantity: 800, FrontURL: "https://example.com/art.png", SlackOptIn: true})
	if err != nil {
		t.Fatal(err)
	}
	runs, _ := s.PrintRuns(ctx, u.ID)
	r := runs[0]
	if !r.SlackOptIn {
		t.Fatal("lost initial opt-in")
	}
	r, err = s.ActPrintRun(ctx, r.ID, PrintAction{Version: r.Version, ActorID: u.ID, Action: "reject", Note: "Private delivery instructions; please revise"})
	if err != nil {
		t.Fatal(err)
	}
	var body string
	if err = s.pool.QueryRow(ctx, `select body from print_notifications where channel='slack' order by id desc limit 1`).Scan(&body); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body, "Private delivery") {
		t.Fatal("private notes copied into Slack")
	}
	if !strings.Contains(body, "Rejected") {
		t.Fatal("Slack must use a human stage label")
	}
	var subject string
	if err = s.pool.QueryRow(ctx, `select subject from print_notifications where channel='email' order by id desc limit 1`).Scan(&subject); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(subject, "Rejected") {
		t.Fatalf("raw stage in subject: %s", subject)
	}
	_, err = s.ActPrintRun(ctx, r.ID, PrintAction{Version: r.Version, ActorID: u.ID, Action: "preferences", SlackOptIn: false})
	if err != nil {
		t.Fatal(err)
	}
	s.DeliverPrintNotifications(ctx, "slack", func(PrintNotification) error { t.Fatal("opted-out Slack job delivered"); return nil })
}

func TestPrintNotificationLeaseAcrossWorkers(t *testing.T) {
	s := newStore(t)
	u := mkUser(t, s, "lease-demo", "lease@x.com")
	s.CreateSubmission(ctx, Submission{UserID: u.ID, Program: "Demo", Title: "Demo", Kind: "sticker", Quantity: 50, FileURL: "https://example.com/art.png"})
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- s.DeliverPrintNotifications(ctx, "email", func(PrintNotification) error { close(started); <-release; return nil })
	}()
	<-started
	if err := s.DeliverPrintNotifications(ctx, "email", func(PrintNotification) error { t.Error("second worker delivered a leased message"); return nil }); err != nil {
		t.Error(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

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
			due := time.Date(2026, 12, 1, 0, 30, 0, 0, time.FixedZone("Demo east", 14*60*60))
			p.RequiredBy = &due
			r, err = s.ActPrintRun(ctx, r.ID, PrintAction{Version: r.Version, Action: "link", ActorID: u.ID, SKU: it.SKU, ItemID: it.ID, ConfirmationURL: "https://example.com/confirmation", Evidence: []swag.PrintPOEvidence{p}})
			if err != nil {
				t.Fatal(err)
			}
			r, err = s.ActPrintRun(ctx, r.ID, PrintAction{Version: r.Version, Action: "observe", Auto: true, Evidence: []swag.PrintPOEvidence{p}})
			if err != nil || r.Stage != swag.PrintOrdered {
				t.Fatalf("ordered %+v %v", r, err)
			}
			if r.ETA != "2026-11-30" {
				t.Fatalf("ETA must use UTC date: %s", r.ETA)
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
			var firstNotices int
			if err := s.pool.QueryRow(ctx, `select count(*) from print_notifications where run_id=$1 and channel='shipment_email'`, r.ID).Scan(&firstNotices); err != nil {
				t.Fatal(err)
			}
			want := 0
			if country == "US" {
				want = 1
			}
			if firstNotices != want {
				t.Fatalf("first parcel notifications %d want %d", firstNotices, want)
			}
			r, _ = s.PrintRun(ctx, r.ID)
			p.Completed = false
			r, err = s.ActPrintRun(ctx, r.ID, PrintAction{Version: r.Version, Action: "observe", Auto: true, Evidence: []swag.PrintPOEvidence{p}})
			if err != nil || r.Stage != swag.PrintAvailable || !r.Reconcile {
				t.Fatalf("reopened %+v %v", r, err)
			}
			p.Completed = true
			corrected := time.Now().Add(-5 * time.Minute)
			p.CompletedAt = &corrected
			r, err = s.ActPrintRun(ctx, r.ID, PrintAction{Version: r.Version, Action: "observe", Auto: true, Evidence: []swag.PrintPOEvidence{p}})
			if err != nil || !r.Reconcile {
				t.Fatal("corrected completion must remain queued for HQ review")
			}
			r, err = s.ActPrintRun(ctx, r.ID, PrintAction{Version: r.Version, Action: "review_receipt", ActorID: u.ID, Note: "Warehouse confirmed the corrected completion record; no partial quantity is claimed.", ETASource: "Demo warehouse confirmation"})
			if err != nil || r.Stage != swag.PrintAvailable || r.Reconcile {
				t.Fatalf("receipt review %+v %v", r, err)
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
