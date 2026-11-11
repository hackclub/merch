package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/hackclub/merch/internal/store"
	"github.com/hackclub/merch/internal/swag"
)

func TestPrintRequesterEvidencePrivacyAndHistory(t *testing.T) {
	h := newHarness(t)
	owner, admin := h.client("owner@x.com"), h.client(adminEmail)
	var source store.PrintRequest
	h.do(owner, "POST", "/api/print-requests", map[string]any{"product": "postcard_4x6", "title": "Demo cards", "program": "Demo", "deadline": "Next month", "note": "Welcome kits", "quantity": 1000, "front_url": "https://example.com/art.png", "initial_quantity": 25, "address": usAddr}, &source)
	runs, _ := h.store.PrintRuns(context.Background(), 0)
	run := runs[0]
	approved := time.Now().Add(-30 * time.Minute)
	act := func(a store.PrintAction) {
		t.Helper()
		a.Version = run.Version
		var err error
		run, err = h.store.ActPrintRun(context.Background(), run.ID, a)
		if err != nil {
			t.Fatal(err)
		}
	}
	act(store.PrintAction{Action: "approve", ActorID: run.UserID, ApprovedQuantity: 1000, Specifications: "Demo specification", Now: approved})
	created := approved.Add(time.Minute)
	proof := swag.PrintPOEvidence{PrintPOLink: swag.PrintPOLink{POID: 42, LineID: 10, SKU: "Demo/Card"}, OrderNumber: "DEMO-PRIVATE-ORDER", Supplier: "DEMO-PRIVATE-SUPPLIER", Quantity: 1000, CreatedAt: &created, SyncedAt: time.Now(), PolledAt: time.Now()}
	act(store.PrintAction{Action: "link", ActorID: run.UserID, SKU: proof.SKU, ConfirmationURL: "https://example.com/private-confirmation", Evidence: []swag.PrintPOEvidence{proof}})
	proof.Draft = true // same-stage observations should stay out of the requester's activity
	act(store.PrintAction{Action: "observe", Auto: true, Evidence: []swag.PrintPOEvidence{proof}})
	path := fmt.Sprintf("/api/print-runs/%d", run.ID)
	for _, endpoint := range []string{"/api/print-runs", path} {
		var got map[string]any
		if c := h.do(owner, "GET", endpoint, nil, &got); c != 200 {
			t.Fatal(c)
		}
		raw, _ := json.Marshal(got)
		for _, private := range []string{`"po_id"`, `"line_id"`, `"order_number"`, `"supplier"`, `"po_links"`, "DEMO-PRIVATE", "private-confirmation"} {
			if strings.Contains(string(raw), private) {
				t.Errorf("%s leaked %s", endpoint, private)
			}
		}
		if endpoint == path {
			stock := got["stock"].(map[string]any)
			if stock["max_age_seconds"] != skuCacheTTL.Seconds() {
				t.Errorf("missing SKU cache lifetime: %+v", stock)
			}
			history := got["history"].([]any)
			if len(history) != 2 {
				t.Errorf("requester history contains internal activity: %+v", history)
			}
			evidence := got["run"].(map[string]any)["links"].([]any)[0].(map[string]any)
			if evidence["status"] != "Needs HQ review: draft or deleted PO" {
				t.Errorf("missing public evidence status %+v", evidence)
			}
		}
	}
	var full map[string]any
	h.do(admin, "GET", path, nil, &full)
	raw, _ := json.Marshal(full)
	if !strings.Contains(string(raw), "DEMO-PRIVATE-ORDER") {
		t.Fatal("HQ lost its evidence")
	}
	var queue map[string]any
	h.do(admin, "GET", "/api/admin/print-runs", nil, &queue)
	if queue["po_linking_available"] != false {
		t.Fatal("HQ queue must explain unavailable PO linking")
	}
}

func TestPrintNotificationsSkipUnconfiguredSlack(t *testing.T) {
	h := newHarness(t)
	owner := h.client("owner@x.com")
	var source store.PrintRequest
	if c := h.do(owner, "POST", "/api/print-requests", map[string]any{"product": "postcard_4x6", "title": "Demo cards", "program": "Demo", "deadline": "Next month", "note": "Welcome kits", "quantity": 1000, "front_url": "https://example.com/art.png", "initial_quantity": 25, "address": usAddr, "slack_opt_in": true}, &source); c != 201 {
		t.Fatal(c)
	}
	if source.SlackOptIn {
		t.Fatal("accepted Slack opt-in without configured delivery")
	}
}

func TestUSPrintFirstShipmentReceivedEmailOnce(t *testing.T) {
	h, m := newMailHarness(t)
	owner, admin := h.client("owner@x.com"), h.client(adminEmail)
	var source store.PrintRequest
	h.do(owner, "POST", "/api/print-requests", map[string]any{"product": "postcard_4x6", "title": "Demo cards", "program": "Demo", "deadline": "Next month", "note": "Welcome kits", "quantity": 1000, "front_url": "https://example.com/art.png", "initial_quantity": 25, "address": usAddr}, &source)
	runs, _ := h.store.PrintRuns(context.Background(), 0)
	run := runs[0]
	product, _ := swag.PrintProductByKey("postcard_4x6")
	item := product.Item("Pri/A", "Demo cards", "linen", true)
	item.Visible = false
	item.Images = []string{"https://example.com/preview.jpg"}
	item, err := h.store.CreateItem(context.Background(), item)
	if err != nil {
		t.Fatal(err)
	}
	approved := time.Now().Add(-30 * time.Minute)
	created := approved.Add(time.Minute)
	act := func(a store.PrintAction) {
		t.Helper()
		a.Version = run.Version
		var err error
		run, err = h.store.ActPrintRun(context.Background(), run.ID, a)
		if err != nil {
			t.Fatal(err)
		}
	}
	act(store.PrintAction{Action: "approve", ActorID: run.UserID, ApprovedQuantity: 1000, Specifications: "Demo", Now: approved})
	proof := swag.PrintPOEvidence{PrintPOLink: swag.PrintPOLink{POID: 42, LineID: 10, SKU: item.SKU}, Quantity: 1000, CreatedAt: &created, Completed: true, SyncedAt: time.Now(), PolledAt: time.Now()}
	act(store.PrintAction{Action: "link", ActorID: run.UserID, SKU: item.SKU, ItemID: item.ID, ConfirmationURL: "https://example.com/confirmation", Evidence: []swag.PrintPOEvidence{proof}})
	act(store.PrintAction{Action: "observe", Auto: true, Evidence: []swag.PrintPOEvidence{proof}})
	version := run.Version
	path := fmt.Sprintf("/api/print-runs/%d/actions", run.ID)
	for i := 0; i < 2; i++ {
		if code := h.do(admin, "POST", path, map[string]any{"action": "release", "version": version}, &run); code != 200 {
			t.Fatal(code)
		}
	}
	parcel, err := h.store.GetRequest(context.Background(), *run.ShipmentRequestID)
	if err != nil {
		t.Fatal(err)
	}
	worker := &server{Deps: Deps{Store: h.store, Config: h.cfg, Mailer: m, Now: time.Now, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}}
	// Restore the old parcel acknowledgment, independently of the Available email.
	worker.processPrintJobs(context.Background())
	worker.processPrintJobs(context.Background())
	wanted := receivedEmail(parcel, h.cfg.BaseURL)
	count := 0
	for _, sent := range m.emails() {
		if sent.Subject == wanted.Subject {
			count++
			if sent.Body != wanted.Body || sent.Preview != wanted.Preview || sent.IdempotencyKey != fmt.Sprintf("merch-request-%d-received", parcel.ID) {
				t.Fatal("parcel acknowledgment changed or lost its stable key")
			}
		}
	}
	if count != 1 {
		t.Fatalf("parcel acknowledgment sent %d times", count)
	}
	logs, err := h.store.ListEmails(context.Background(), store.EmailFilter{RequestID: parcel.ID, Kind: "received", Limit: 10})
	if err != nil || len(logs) != 1 {
		t.Fatalf("parcel email history %+v %v", logs, err)
	}
	var listed struct {
		Requests []store.Request `json:"requests"`
	}
	h.do(admin, "GET", "/api/admin/requests?status=pending", nil, &listed)
	found := false
	for _, p := range listed.Requests {
		if p.ID == parcel.ID {
			found = p.PrintRunID != nil && *p.PrintRunID == run.ID
		}
	}
	if !found {
		t.Fatal("admin shipment list lost the print-run back link")
	}
}
