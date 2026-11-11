package swag

import (
	"errors"
	"testing"
	"time"
)

func TestPrintRunNeedsPoll(t *testing.T) {
	now := time.Now().UTC()
	recent, older := now.Add(-time.Minute), now.Add(-15*time.Minute)
	base := PrintRunState{Stage: PrintAvailable, PrintRunData: PrintRunData{LastPollAt: &recent}, Links: []PrintPOEvidence{{Completed: true}}}
	for _, tc := range []struct {
		name string
		edit func(*PrintRunState)
		want bool
	}{
		{"completed release", func(*PrintRunState) {}, false},
		{"never polled", func(r *PrintRunState) { r.LastPollAt = nil }, true},
		{"cadence boundary", func(r *PrintRunState) { r.LastPollAt = &older }, true},
		{"reconciliation", func(r *PrintRunState) { r.Reconcile = true }, true},
		{"stale", func(r *PrintRunState) { r.SourceStale = true }, true},
		{"error", func(r *PrintRunState) { r.PollError = "source unavailable" }, true},
		{"reopened", func(r *PrintRunState) { r.Links = []PrintPOEvidence{{}} }, true},
		{"deleted", func(r *PrintRunState) { r.Links = []PrintPOEvidence{{Completed: true, Deleted: true}} }, true},
		{"draft", func(r *PrintRunState) { r.Links = []PrintPOEvidence{{Completed: true, Draft: true}} }, true},
		{"unlinked", func(r *PrintRunState) { r.Links = nil }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := base
			tc.edit(&r)
			if got := PrintRunNeedsPoll(r, now); got != tc.want {
				t.Fatalf("%v want %v", got, tc.want)
			}
		})
	}
	for _, stage := range []PrintStage{PrintSubmitted, PrintApproved, PrintOrdered, PrintInTransit, PrintReceived, PrintRejected, PrintCancelled} {
		r := base
		r.Stage = stage
		want := stage == PrintApproved || stage == PrintOrdered || stage == PrintInTransit || stage == PrintReceived
		if PrintRunNeedsPoll(r, now) != want {
			t.Fatalf("poll stage %s", stage)
		}
		if (CheckPrintReorder(stage) == nil) != (stage == PrintRejected || stage == PrintCancelled) {
			t.Fatalf("reorder stage %s", stage)
		}
	}
	if err := CheckPrintReorder(PrintAvailable); err != nil {
		t.Fatal(err)
	}
}

func TestPrintActionEventAndNoticeDecisions(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		name          string
		stage         PrintStage
		action        PrintAction
		origin        string
		parcel        bool
		source, event string
		notify        bool
		notice        PrintNoticeKind
	}{
		{"HQ update", PrintApproved, PrintAction{Action: "update", Note: "Public update", ETASource: "Demo HQ"}, "staff", false, "HQ", "update", true, PrintNoticeUpdate},
		{"private update", PrintApproved, PrintAction{Action: "update", InternalNote: "Private"}, "staff", false, "HQ", "update", false, PrintNoticeUpdate},
		{"requester preference", PrintApproved, PrintAction{Action: "preferences", ActorID: 1}, "staff", false, "requester", "preferences", false, PrintNoticeUpdate},
		{"approval", PrintSubmitted, PrintAction{Action: "approve", ApprovedQuantity: 800, Specifications: "Checked"}, "staff", false, "HQ", "approve", true, PrintNoticeStage},
		{"legacy audit", PrintApproved, PrintAction{Action: "audit_legacy", ApprovedQuantity: 800, Specifications: "Checked", Note: "Original order reviewed"}, "legacy", false, "HQ", "audit_legacy", false, PrintNoticeUpdate},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := PrintRunState{UserID: 1, Stage: tc.stage, Origin: tc.origin, PrintRunData: PrintRunData{LegacyReview: tc.action.Action == "audit_legacy"}}
			next, err := ApplyPrintAction(r, tc.action, PrintActor{ID: tc.action.ActorID, Admin: true}, now)
			if err != nil || next.EventSource != tc.source || next.EventAction != tc.event || next.Notify != tc.notify || next.Notice != tc.notice {
				t.Fatalf("%+v %v", next, err)
			}
		})
	}
	_, err := ApplyPrintAction(PrintRunState{UserID: 1, Stage: PrintSubmitted}, PrintAction{Action: "approve"}, PrintActor{ID: 1}, now)
	var actionErr *PrintActionError
	if !errors.As(err, &actionErr) || !actionErr.Forbidden || !errors.Is(err, ErrPrintAction) {
		t.Fatalf("forbidden action type: %v", err)
	}
}

func TestPrintWarehouseAndReleaseNoticeDecisions(t *testing.T) {
	now := time.Now().UTC()
	approved, created := now.Add(-2*time.Hour), now.Add(-time.Hour)
	due := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	proof := PrintPOEvidence{PrintPOLink: PrintPOLink{POID: 12, LineID: 34, SKU: "Demo/Card"}, Quantity: 800, CreatedAt: &created, RequiredBy: &due, SyncedAt: now, PolledAt: now}
	base := PrintRunState{UserID: 1, Stage: PrintOrdered, Origin: "staff", PrintRunData: PrintRunData{ApprovedAt: &approved, ApprovedQuantity: 800, SKU: "Demo/Card", ItemID: 1, ETA: "2026-11-30"}, Links: []PrintPOEvidence{proof}, ListingReady: true,
		Request: PrintRunRequest{Quantity: 800, InitialQuantity: 25, Address: &Address{FirstName: "Demo", LastName: "Person", Line1: "123 Example Lane", City: "Exampleville", State: "VT", PostalCode: "05401", Country: "US"}}}
	for _, tc := range []struct {
		name              string
		stage             PrintStage
		origin            string
		parcel            bool
		completed         bool
		wantEvent         string
		wantNotify, first bool
		kind              PrintNoticeKind
	}{
		{"PO ETA", PrintOrdered, "staff", false, false, "eta_update", true, false, PrintNoticePOETA},
		{"automatic order", PrintApproved, "staff", false, false, "observe", true, false, PrintNoticeStage},
		{"automatic receipt", PrintOrdered, "staff", false, true, "observe", true, false, PrintNoticeStage},
		{"new release", PrintReceived, "staff", false, true, "release", true, true, PrintNoticeStage},
		{"legacy existing parcel", PrintReceived, "legacy", true, true, "release", false, false, PrintNoticeStage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := base
			r.Stage = tc.stage
			r.Origin = tc.origin
			if tc.parcel {
				id := int64(9)
				r.ShipmentRequestID = &id
			}
			p := proof
			p.Completed = tc.completed
			r.Links = []PrintPOEvidence{p}
			a := PrintAction{Action: "observe", Auto: true, Evidence: []PrintPOEvidence{p}}
			actor := PrintActor{Automatic: true}
			source := "warehouse PO copy"
			if tc.wantEvent == "release" {
				a = PrintAction{Action: "release", Sellable: 800, ReleaseStatus: StatusPending}
				actor = PrintActor{ID: 2, Admin: true}
				source = "HQ"
			}
			got, err := ApplyPrintAction(r, a, actor, now)
			if err != nil || got.EventSource != source || got.EventAction != tc.wantEvent || got.Notify != tc.wantNotify || got.NotifyFirstShipment != tc.first || got.Notice != tc.kind {
				t.Fatalf("%+v %v", got, err)
			}
		})
	}
}
