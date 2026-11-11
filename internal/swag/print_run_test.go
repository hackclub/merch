package swag

import (
	"testing"
	"time"
)

func TestPrintRunTransitions(t *testing.T) {
	for _, tc := range []struct {
		from, to PrintStage
		ok       bool
	}{
		{PrintSubmitted, PrintApproved, true}, {PrintSubmitted, PrintOrdered, false},
		{PrintApproved, PrintOrdered, true}, {PrintOrdered, PrintInTransit, true},
		{PrintOrdered, PrintReceived, true}, {PrintInTransit, PrintReceived, true},
		{PrintReceived, PrintAvailable, true}, {PrintOrdered, PrintAvailable, false},
		{PrintSubmitted, PrintRejected, true}, {PrintApproved, PrintRejected, true},
		{PrintOrdered, PrintRejected, false}, {PrintAvailable, PrintReceived, false},
		{PrintSubmitted, PrintCancelled, true}, {PrintOrdered, PrintCancelled, true},
		{PrintRejected, PrintApproved, false}, {PrintApproved, PrintApproved, false},
		{"unknown", PrintCancelled, false}, {PrintAvailable, PrintCancelled, false},
	} {
		t.Run(string(tc.from)+"/"+string(tc.to), func(t *testing.T) {
			if got := CanTransitionPrint(tc.from, tc.to); got != tc.ok {
				t.Fatalf("got %v want %v", got, tc.ok)
			}
		})
	}
}

func TestPrintPOEvidence(t *testing.T) {
	now := time.Date(2026, 11, 11, 16, 0, 0, 0, time.UTC)
	created, completed := now.Add(-48*time.Hour), now.Add(-24*time.Hour)
	base := PrintPOEvidence{PrintPOLink: PrintPOLink{POID: 12, LineID: 34, SKU: "Demo/Card"}, Quantity: 400, CreatedAt: &created, CompletedAt: &completed,
		Completed: true, SyncedAt: now.Add(-time.Minute), PolledAt: now}
	link := PrintPOLink{POID: 12, LineID: 34, SKU: "Demo/Card"}
	for _, tc := range []struct {
		name  string
		edit  func(*PrintPOEvidence)
		valid bool
	}{
		{"valid", func(*PrintPOEvidence) {}, true},
		{"draft", func(p *PrintPOEvidence) { p.Draft = true }, false},
		{"deleted", func(p *PrintPOEvidence) { p.Deleted = true }, false},
		{"wrong PO", func(p *PrintPOEvidence) { p.POID++ }, false},
		{"wrong line", func(p *PrintPOEvidence) { p.LineID++ }, false},
		{"wrong SKU", func(p *PrintPOEvidence) { p.SKU += "/Other" }, false},
		{"unknown creation", func(p *PrintPOEvidence) { p.CreatedAt = nil }, false},
		{"stale", func(p *PrintPOEvidence) { p.SyncedAt = now.Add(-2 * time.Hour) }, false},
		{"no sync", func(p *PrintPOEvidence) { p.SyncedAt = time.Time{} }, false},
		{"future sync", func(p *PrintPOEvidence) { p.SyncedAt = now.Add(time.Minute) }, false},
		{"negative quantity", func(p *PrintPOEvidence) { p.Quantity = -1 }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := base
			tc.edit(&p)
			if got := p.Confirms(link, now, time.Hour); got != tc.valid {
				t.Fatalf("got %v want %v", got, tc.valid)
			}
		})
	}
	// Split POs must cover the approved quantity and every required PO must complete.
	a := base
	a.Completed = false
	a.CompletedAt = nil
	b := base
	b.POID = 13
	b.LineID = 35
	links := []PrintPOLink{link, {POID: 13, LineID: 35, SKU: link.SKU}}
	approval := PrintApproval{Quantity: 800, SKU: link.SKU, At: now.Add(-72 * time.Hour)}
	if got := ObservePrintPOs(PrintApproved, approval, links, []PrintPOEvidence{a, b}, now, time.Hour); got.Stage != PrintOrdered {
		t.Fatalf("split incomplete: %+v", got)
	}
	if got := ObservePrintPOs(PrintOrdered, approval, links, []PrintPOEvidence{base, b}, now, time.Hour); got.Stage != PrintReceived {
		t.Fatalf("split complete: %+v", got)
	}
	mismatch := approval
	older := b
	older.SyncedAt = b.SyncedAt.Add(-time.Minute)
	if got := ObservePrintPOs(PrintOrdered, approval, links, []PrintPOEvidence{base, older}, now, time.Hour); got.Stage != "" || !got.Reconcile {
		t.Fatalf("mixed source snapshots: %+v", got)
	}
	mismatch.Quantity = 700
	if got := ObservePrintPOs(PrintApproved, mismatch, links, []PrintPOEvidence{base, b}, now, time.Hour); got.Stage != "" || !got.Reconcile {
		t.Fatalf("quantity mismatch: %+v", got)
	}
	if got := ObservePrintPOs(PrintApproved, approval, append(links, link), []PrintPOEvidence{base, b}, now, time.Hour); got.Stage != "" || !got.Reconcile {
		t.Fatalf("duplicate link: %+v", got)
	}
	if got := ObservePrintPOs(PrintOrdered, approval, links, []PrintPOEvidence{base}, now, time.Hour); got.Stage != "" {
		t.Fatalf("missing PO: %+v", got)
	}
	if got := ObservePrintPOs(PrintSubmitted, approval, links, []PrintPOEvidence{base, b}, now, time.Hour); got.Stage != "" {
		t.Fatalf("PO cannot approve: %+v", got)
	}
	if got := ObservePrintPOs(PrintAvailable, approval, links, []PrintPOEvidence{a, b}, now, time.Hour); !got.Reconcile || got.Stage != "" {
		t.Fatalf("reopened: %+v", got)
	}
	if got := ObservePrintPOs(PrintReceived, approval, links, []PrintPOEvidence{base, b}, now, time.Hour); got.Stage != "" {
		t.Fatalf("duplicate receipt: %+v", got)
	}
	// Late completion skips unrecorded dispatch and avoids a burst of historical stages.
	if got := ObservePrintPOs(PrintApproved, approval, links, []PrintPOEvidence{base, b}, now, time.Hour); got.Stage != PrintReceived {
		t.Fatalf("late completion: %+v", got)
	}
	lateApproval := approval
	lateApproval.At = now
	if got := ObservePrintPOs(PrintApproved, lateApproval, links, []PrintPOEvidence{base, b}, now, time.Hour); !got.Reconcile || got.Stage != "" {
		t.Fatalf("order predates approval: %+v", got)
	}
	wrongSKU := approval
	wrongSKU.SKU = "Demo/Other"
	if got := ObservePrintPOs(PrintApproved, wrongSKU, links, []PrintPOEvidence{base, b}, now, time.Hour); !got.Reconcile || got.Stage != "" {
		t.Fatalf("wrong approved SKU: %+v", got)
	}
}

func TestPrintRequestableStock(t *testing.T) {
	for _, tc := range []struct {
		visible              bool
		sellable, held, want int
	}{
		{true, 800, 25, 775}, {false, 800, 25, 0}, {true, 10, 25, 0}, {true, -10, 0, 0},
	} {
		if got := PrintRequestableStock(tc.visible, tc.sellable, tc.held); got != tc.want {
			t.Fatalf("%+v got %d", tc, got)
		}
	}
}
