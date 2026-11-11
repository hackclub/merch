package httpapi

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hackclub/merch/internal/store"
	"github.com/hackclub/merch/internal/swag"
)

func TestPrintMessageWording(t *testing.T) {
	for _, tc := range []struct {
		stage       swag.PrintStage
		label, body string
	}{
		{swag.PrintSubmitted, "Submitted", "We received your Demo cards print request. HQ will review the artwork and quantity. No order has been placed."},
		{swag.PrintApproved, "Approved", "Your Demo cards request is approved for 800. HQ will place the printer order next."},
		{swag.PrintOrdered, "Ordered from printer", "Your linked purchase order confirms 800 Demo cards ordered. Expected warehouse date: 2026-12-01. We will tell you when the printer ships."},
		{swag.PrintInTransit, "In transit", "Your print run has shipped from the printer to the warehouse. Estimated arrival: 2026-12-01. This is not your first parcel."},
		{swag.PrintReceived, "Received at warehouse", "The warehouse marked your linked purchase order complete. Your print run is received at the warehouse. The shop listing awaits release. Your first parcel has not shipped."},
		{swag.PrintAvailable, "Available", "Demo cards is available in the shop. No first shipment was requested."},
		{swag.PrintRejected, "Rejected", "Your Demo cards request was rejected: Please revise No order was placed."},
		{swag.PrintCancelled, "Cancelled", "Your Demo cards print request was cancelled. Please revise No order was placed."},
	} {
		t.Run(string(tc.stage), func(t *testing.T) {
			f := store.PrintMessageFacts{RunID: 42, Stage: tc.stage, Notice: swag.PrintNoticeStage, Title: "Demo cards", RequestedQuantity: 1000, ApprovedQuantity: 800, ETA: "2026-12-01", Note: "Please revise"}
			got := renderPrintMessage(f, "https://example.com/")
			footer := "\n\nView print request: https://example.com/print-requests/42"
			if got.Body != tc.body+footer {
				t.Fatalf("body %q", got.Body)
			}
			subject := "Print #42 · " + tc.label
			if tc.stage == swag.PrintSubmitted {
				subject = "Print request received"
			}
			if got.Subject != subject {
				t.Fatalf("subject %q", got.Subject)
			}
			wantSlack := "Print request #42: " + tc.label + ". Requested 1000; approved 800. Printing status is separate from your first parcel."
			if tc.stage == swag.PrintSubmitted {
				wantSlack = "We received your print request. HQ will review the artwork and quantity. No order has been placed."
			}
			wantSlack += " Expected warehouse date: 2026-12-01 (estimate)." + footer
			if got.SlackBody != wantSlack || strings.Contains(got.SlackBody, "Please revise") {
				t.Fatalf("Slack %q", got.SlackBody)
			}
			if got.ShipmentNote != "First shipment from print run #42" {
				t.Fatal(got.ShipmentNote)
			}
		})
	}
}

func TestPrintUpdateAndParcelWording(t *testing.T) {
	parcel := int64(9)
	base := store.PrintMessageFacts{RunID: 42, Stage: swag.PrintReceived, Notice: swag.PrintNoticeUpdate, Note: "Confirmed", ETASource: "Demo HQ", ETA: "2026-12-01", UpdateETA: "2026-12-01"}
	for _, tc := range []struct {
		name      string
		kind      swag.PrintNoticeKind
		appendETA bool
		want      string
	}{
		{"public update", swag.PrintNoticeUpdate, false, "Confirmed"},
		{"manual ETA", swag.PrintNoticeUpdate, true, "Confirmed\nExpected warehouse arrival: 2026-12-01 (Demo HQ). This is not a delivery promise."},
		{"receipt review", swag.PrintNoticeReceiptReview, false, "Confirmed Source: Demo HQ. Received quantity unavailable."},
		{"PO ETA", swag.PrintNoticePOETA, false, "Expected warehouse date for your print run is now 2026-12-01 (linked PO required-by). This is an estimate, not a delivery promise."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := base
			f.Notice = tc.kind
			f.AppendETA = tc.appendETA
			got := renderPrintMessage(f, "https://example.com")
			if got.Body != tc.want+"\n\nView print request: https://example.com/print-requests/42" {
				t.Fatal(got.Body)
			}
		})
	}
	f := base
	f.Stage = swag.PrintAvailable
	f.Notice = swag.PrintNoticeStage
	f.Title = "Demo cards"
	f.FirstQuantity = 25
	f.ShipmentRequestID = &parcel
	f.ShipmentStatus = swag.StatusAwaitingPayment
	f.ReleaseStatus = swag.StatusAwaitingPayment
	got := renderPrintMessage(f, "https://example.com")
	if !strings.HasPrefix(got.Body, "Demo cards is available in the shop. Your first 25 are in shipment request #9, awaiting postage payment.") || !strings.Contains(got.SlackBody, "First shipment request #9: awaiting_payment.") {
		t.Fatalf("%+v", got)
	}
	f.Stage = swag.PrintCancelled
	f.PrinterCancelled = true
	if !strings.Contains(renderPrintMessage(f, "https://example.com").Body, "Printer confirmed cancellation.") {
		t.Fatal("lost printer cancellation wording")
	}
}

func TestPrintActionErrorStatus(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{
		{&swag.PrintActionError{Reason: "HQ review required", Forbidden: true}, 403},
		{&swag.PrintActionError{Reason: "finish the current run first"}, 422},
		{fmt.Errorf("%w: add an update", swag.ErrPrintAction), 422},
	} {
		w := httptest.NewRecorder()
		writePrintActionError(w, tc.err)
		if w.Code != tc.status {
			t.Fatalf("%v: %d want %d", tc.err, w.Code, tc.status)
		}
	}
}
