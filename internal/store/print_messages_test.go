package store

import (
	"fmt"
	"testing"
)

// Store tests supply facts-only output; wording is covered by the HTTP renderer tests.
var testPrintMessages = PrintMessages{BaseURL: "https://example.com", Render: func(f PrintMessageFacts, _ string) PrintMessage {
	return PrintMessage{Subject: "stage=" + string(f.Stage), Body: fmt.Sprintf("run=%d stage=%s", f.RunID, f.Stage), SlackBody: "stage=" + string(f.Stage), ShipmentNote: fmt.Sprintf("run=%d", f.RunID), ReorderNote: fmt.Sprintf("run=%d", f.NewRunID)}
}}

func TestPrintNotificationKeyMatchesExistingFormat(t *testing.T) {
	// This is the MD5 format previously queued by Postgres, including URL normalization.
	if got := printNotificationKey("https://example.com/", 42, "email"); got != "merch-print-b78ba7d1c6590fff156b11d5e2916582" {
		t.Fatalf("key %q", got)
	}
}

func TestQueuedPrintMessagesArePreparedBeforeDelivery(t *testing.T) {
	s := newStore(t)
	u := mkUser(t, s, "message-demo", "message@x.com")
	if _, err := s.CreatePrintRequest(ctx, PrintRequest{UserID: u.ID, Product: "postcard_4x6", Title: "Demo", Quantity: 800, FrontURL: "https://example.com/art.png"}, testPrintMessages); err != nil {
		t.Fatal(err)
	}
	runs, err := s.PrintRuns(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	r := runs[0]
	var body, key string
	var eventID int64
	if err = s.pool.QueryRow(ctx, `select body,idempotency_key,event_id from print_notifications where run_id=$1`, r.ID).Scan(&body, &key, &eventID); err != nil || body != fmt.Sprintf("run=%d stage=submitted", r.ID) || key != printNotificationKey(testPrintMessages.BaseURL, eventID, "email") {
		t.Fatalf("prepared %q %q %v", body, key, err)
	}
	if _, err = s.ActPrintRun(ctx, r.ID, PrintAction{Action: "reject", Version: r.Version, ActorID: u.ID, Note: "Please revise"}, testPrintMessages); err != nil {
		t.Fatal(err)
	}
	if err = s.DeliverPrintNotifications(ctx, "email", func(PrintNotification) error { return nil }); err != nil {
		t.Fatal(err)
	}
	rows, err := s.pool.Query(ctx, `select kind from sent_emails order by id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for _, want := range []string{"print_submitted", "print_rejected"} {
		var kind string
		if !rows.Next() {
			t.Fatal("missing sent email")
		}
		if err = rows.Scan(&kind); err != nil || kind != want {
			t.Fatalf("kind %q want %s: %v", kind, want, err)
		}
	}
}
