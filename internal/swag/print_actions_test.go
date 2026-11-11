package swag

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// Valid fields isolate permission and stage rules. Field guards have their own cases below.
func TestApplyPrintActionStageAndActorMatrix(t *testing.T) {
	now := time.Date(2026, 11, 11, 16, 0, 0, 0, time.UTC)
	approved, created := now.Add(-time.Hour), now.Add(-30*time.Minute)
	address := &Address{FirstName: "Demo", LastName: "Person", Line1: "123 Example Lane", City: "Exampleville", State: "VT", PostalCode: "05401", Country: "US"}
	stages := []PrintStage{PrintSubmitted, PrintApproved, PrintOrdered, PrintInTransit, PrintReceived, PrintAvailable, PrintRejected, PrintCancelled}
	actions := []struct {
		name, stages string
		requester    bool
	}{
		{"approve", "submitted", false}, {"reject", "submitted approved", false},
		{"cancel", "submitted approved ordered in_transit", true}, {"dispatch", "ordered", false},
		{"update", "submitted approved ordered in_transit received available", false},
		{"preferences", "*", true}, {"first_shipment", "submitted approved ordered in_transit received", true},
		{"retry_notifications", "*", false}, {"resolve_notification", "*", false},
		{"review_order", "approved ordered in_transit", false}, {"review_receipt", "received available", false},
		{"link", "approved ordered", false}, {"replace_links", "approved ordered", false},
		{"observe", "*", false}, {"release", "received", false}, {"audit_legacy", "*", false},
		{"unknown", "", false},
	}
	actors := []struct {
		name  string
		actor PrintActor
	}{
		{"HQ", PrintActor{ID: 1, Admin: true}}, {"requester", PrintActor{ID: 1}},
		{"other requester", PrintActor{ID: 2}}, {"PO worker", PrintActor{Automatic: true}},
	}
	for _, action := range actions {
		for _, stage := range stages {
			for _, who := range actors {
				t.Run(action.name+"/"+string(stage)+"/"+who.name, func(t *testing.T) {
					r := PrintRunState{UserID: 1, Stage: stage, Origin: "staff", PrintRunData: PrintRunData{ApprovedQuantity: 800, ApprovedAt: &approved, Revision: 1, SKU: "Demo/Card", ItemID: 1, ReviewedExistingOrder: true}, Request: PrintRunRequest{Product: "postcard_4x6", Quantity: 800, InitialQuantity: 25, Address: address}, ListingReady: true, NotificationUncertain: true, LegacyShipmentMatches: true}
					p := PrintPOEvidence{PrintPOLink: PrintPOLink{POID: 12, LineID: 34, SKU: "Demo/Card"}, Quantity: 800, CreatedAt: &created, SyncedAt: now, PolledAt: now, Completed: true}
					a := PrintAction{Action: action.name, ActorID: who.actor.ID, ApprovedQuantity: 800, Specifications: "Art and quantity checked", Note: "Demo public update", ETASource: "Demo warehouse", ConfirmationURL: "https://example.com/confirmation", CancellationDisposition: "No goods dispatched", SKU: "Demo/Card", ItemID: 1, Evidence: []PrintPOEvidence{p}, InitialQuantity: 25, Address: address, ReleaseStatus: StatusPending, Sellable: 800, DeliveryOutcome: "sent", Auto: who.actor.Automatic}
					if action.name == "review_receipt" || action.name == "observe" || action.name == "replace_links" || action.name == "release" {
						r.Links = []PrintPOEvidence{p}
					}
					if action.name == "audit_legacy" {
						r.LegacyReview = true
					}
					want := action.stages == "*" || strings.Contains(" "+action.stages+" ", " "+string(stage)+" ")
					if action.name == "observe" {
						want = who.actor.Automatic
					} else {
						want = want && (who.actor.Admin || who.name == "requester" && action.requester)
						if who.name == "requester" && action.name == "cancel" && stage != PrintSubmitted && stage != PrintApproved {
							want = false
						}
					}
					before := r
					before.Links = append([]PrintPOEvidence(nil), r.Links...)
					next, err := ApplyPrintAction(r, a, who.actor, now)
					if (err == nil) != want {
						t.Fatalf("allowed=%v want %v: %v", err == nil, want, err)
					}
					if !reflect.DeepEqual(before, r) {
						t.Fatal("policy mutated its input")
					}
					if want {
						expected := stage
						switch action.name {
						case "approve":
							expected = PrintApproved
						case "reject":
							expected = PrintRejected
						case "cancel":
							expected = PrintCancelled
						case "dispatch":
							expected = PrintInTransit
						case "release":
							expected = PrintAvailable
						case "observe":
							if stage == PrintApproved || stage == PrintOrdered || stage == PrintInTransit {
								expected = PrintReceived
							}
						}
						if next.Run.Stage != expected {
							t.Fatalf("next stage %s want %s", next.Run.Stage, expected)
						}
					}
				})
			}
		}
	}
}

func TestApplyPrintActionRequiredFields(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		action  string
		stage   PrintStage
		message string
	}{
		{"approve", PrintSubmitted, "review artwork"}, {"reject", PrintSubmitted, "reason and revision"},
		{"cancel", PrintSubmitted, "cancellation reason"}, {"update", PrintApproved, "add an update"},
		{"first_shipment", PrintApproved, "valid first quantity"}, {"resolve_notification", PrintApproved, "provider delivery evidence"},
		{"review_order", PrintApproved, "quantity, specifications"}, {"review_receipt", PrintReceived, "warehouse confirmation"},
		{"link", PrintApproved, "printer confirmation"}, {"replace_links", PrintApproved, "printer confirmation"},
		{"release", PrintReceived, "confirmed sellable stock"}, {"audit_legacy", PrintReceived, "original order"},
	} {
		t.Run(tc.action, func(t *testing.T) {
			_, err := ApplyPrintAction(PrintRunState{UserID: 1, Stage: tc.stage}, PrintAction{Action: tc.action}, PrintActor{ID: 1, Admin: true}, now)
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("%v want %q", err, tc.message)
			}
		})
	}
}
