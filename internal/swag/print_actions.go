package swag

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var ErrPrintAction = errors.New("invalid print action")

type PrintActionError struct {
	Reason    string
	Forbidden bool
}

func (e *PrintActionError) Error() string {
	return e.Reason
}
func (e *PrintActionError) Unwrap() error { return ErrPrintAction }

type PrintNoticeKind string

const (
	PrintNoticeStage         PrintNoticeKind = "stage"
	PrintNoticeUpdate        PrintNoticeKind = "update"
	PrintNoticeReceiptReview PrintNoticeKind = "receipt_review"
	PrintNoticePOETA         PrintNoticeKind = "po_eta"
)

// PrintActor comes from trusted server identity, never request JSON.
type PrintActor struct {
	ID        int64
	Admin     bool
	Automatic bool
}
type PrintRunRequest struct {
	Product                   string
	Quantity, InitialQuantity int
	Address                   *Address
}

// Listing, parcel and notification facts come from the caller.
type PrintRunState struct {
	PrintRunData
	UserID            int64
	Stage             PrintStage
	Origin            string
	Request           PrintRunRequest
	Links             []PrintPOEvidence
	InternalNote      string
	ShipmentRequestID *int64
	ShipmentStatus    Status

	ListingReady          bool
	NotificationUncertain bool
	LegacyShipmentMatches bool
	LegacyListingVisible  bool
}
type PrintActionResult struct {
	Run                                   PrintRunState
	Notify, RecordEvent                   bool
	Notice                                PrintNoticeKind
	AppendETA                             bool
	EventSource, EventAction              string
	NotifyFirstShipment, SaveConfirmation bool
}

type PrintRunData struct {
	DeliveryQuantity      int        `json:"delivery_quantity"`
	DeliveryAddress       *Address   `json:"delivery_address,omitempty"`
	ReviewedExistingOrder bool       `json:"reviewed_existing_order"`
	ApprovedQuantity      int        `json:"approved_quantity"`
	ApprovedAt            *time.Time `json:"approved_at"`
	ApprovedBy            int64      `json:"approved_by,omitempty"` // admin user id; hidden from requesters
	Specifications        string     `json:"specifications"`
	Revision              int        `json:"revision"`
	SKU                   string     `json:"sku"`
	ItemID                int64      `json:"item_id"`
	PublicNote            string     `json:"public_note"`
	Tracking              string     `json:"tracking"`
	ETA                   string     `json:"eta"`
	ETASource             string     `json:"eta_source"`
	DispatchedAt          *time.Time `json:"dispatched_at"`
	Reconcile             bool       `json:"reconcile"`
	ReceiptCorrection     bool       `json:"receipt_correction"`
	ReceiptReviewDigest   string     `json:"receipt_review_digest,omitempty"`
	LegacyReview          bool       `json:"legacy_review"`
	LegacyStatus          string     `json:"legacy_status"`
	SlackOptIn            bool       `json:"slack_opt_in"`
	ParentID              int64      `json:"parent_id"`
	ParentVersion         int64      `json:"parent_version"`
	LastPollAt            *time.Time `json:"last_poll_at"`
	PollError             string     `json:"poll_error"`
	SourceStale           bool       `json:"source_stale"`
}
type PrintAction struct {
	DispatchedAt            *time.Time        `json:"dispatched_at"`
	InitialQuantity         int               `json:"initial_quantity"`
	Address                 *Address          `json:"address"`
	NotificationID          int64             `json:"notification_id"`
	DeliveryOutcome         string            `json:"delivery_outcome"`
	Version                 int64             `json:"version"`
	Action                  string            `json:"action"`
	ActorID                 int64             `json:"-"`
	ApprovedQuantity        int               `json:"approved_quantity"`
	Specifications          string            `json:"specifications"`
	SKU                     string            `json:"sku"`
	ItemID                  int64             `json:"item_id"`
	Note                    string            `json:"note"`
	InternalNote            string            `json:"internal_note"`
	Tracking                string            `json:"tracking"`
	ETA                     string            `json:"eta"`
	ETASource               string            `json:"eta_source"`
	ConfirmationURL         string            `json:"confirmation_url"`
	CancellationDisposition string            `json:"cancellation_disposition"`
	Evidence                []PrintPOEvidence `json:"-"`
	PollError               string            `json:"-"`
	Auto                    bool              `json:"-"`
	Now                     time.Time         `json:"-"`
	SlackOptIn              bool              `json:"slack_opt_in"`
	ParentID                int64             `json:"parent_id"`
	ReleaseStatus           Status            `json:"-"`
	ReleaseFee              int               `json:"-"`
	Sellable                int               `json:"-"`
	Requester               bool              `json:"-"`
}

func (r *PrintRunState) HasPrinterOrder() bool {
	for _, p := range r.Links {
		if !p.Draft && !p.Deleted && p.CreatedAt != nil {
			return true
		}
	}
	return false
}

// Poll timestamps change every observation; only business evidence needs HQ
// review after receipt. Sort a copy so link order cannot change its identity.
func printEvidenceDigest(proofs []PrintPOEvidence) string {
	copy := append([]PrintPOEvidence(nil), proofs...)
	for i := range copy {
		copy[i].SyncedAt = time.Time{}
		copy[i].PolledAt = time.Time{}
	}
	sort.Slice(copy, func(i, j int) bool {
		if copy[i].POID == copy[j].POID {
			return copy[i].LineID < copy[j].LineID
		}
		return copy[i].POID < copy[j].POID
	})
	b, _ := json.Marshal(copy)
	return fmt.Sprintf("%x", sha256.Sum256(b))
}

func completePrintEvidence(r *PrintRunState, now time.Time) bool {
	if len(r.Links) == 0 {
		return false
	}
	for _, p := range r.Links {
		if p.SKU != r.SKU || !p.Completed || !p.Confirms(p.PrintPOLink, now, time.Hour) || !p.SyncedAt.Equal(r.Links[0].SyncedAt) {
			return false
		}
	}
	return true
}

// AuthorizePrintAction checks which actions this actor may request.
func AuthorizePrintAction(state PrintRunState, a PrintAction, actor PrintActor) error {
	r := &state
	forbidden := func(reason string) error { return &PrintActionError{Reason: reason, Forbidden: true} }
	if actor.Automatic {
		if a.Action != "observe" {
			return forbidden("HQ review required")
		}
	} else if a.Action == "observe" {
		return forbidden("PO observations are automatic")
	} else if !actor.Admin {
		if actor.ID != r.UserID {
			return forbidden("HQ review required")
		}
		switch a.Action {
		case "preferences", "first_shipment", "reorder":
		case "cancel":
			if r.Stage != PrintSubmitted && r.Stage != PrintApproved {
				return forbidden("HQ review required")
			}
			if r.HasPrinterOrder() {
				return forbidden("HQ must confirm printer cancellation")
			}
		default:
			return forbidden("HQ review required")
		}
	}
	if a.Action == "preferences" && actor.ID != r.UserID {
		return forbidden("only the requester can opt in to Slack")
	}
	return nil
}

// ApplyPrintAction returns state and event/message decisions without doing I/O.
func ApplyPrintAction(state PrintRunState, a PrintAction, actor PrintActor, now time.Time) (PrintActionResult, error) {
	if err := AuthorizePrintAction(state, a, actor); err != nil {
		return PrintActionResult{}, err
	}
	state.Links = append([]PrintPOEvidence(nil), state.Links...)
	a.ActorID = actor.ID
	r := &state
	invalid := func(reason string) (PrintActionResult, error) {
		return PrintActionResult{}, fmt.Errorf("%w: %s", ErrPrintAction, reason)
	}
	prev, previousReconcile := r.Stage, r.Reconcile
	previousEvidence := printEvidenceDigest(r.Links)
	notify := false
	notice := PrintNoticeUpdate
	switch a.Action {
	case "approve":
		if r.Stage != PrintSubmitted || (r.Origin == "legacy" && r.LegacyStatus == "printing") || a.ApprovedQuantity < 1 || strings.TrimSpace(a.Specifications) == "" {
			return invalid("review artwork, specifications, quantity and budget first")
		}
		r.Stage = PrintApproved
		r.ApprovedQuantity = a.ApprovedQuantity
		r.Specifications = a.Specifications
		r.ApprovedAt = &now
		r.ApprovedBy = a.ActorID
		r.Revision = 1
		if a.SKU != "" {
			r.SKU = strings.TrimSpace(a.SKU)
		}
	case "reject":
		if r.HasPrinterOrder() {
			return invalid("linked printer order exists; use confirmed cancellation")
		}
		if (r.Stage != PrintSubmitted && r.Stage != PrintApproved) || strings.TrimSpace(a.Note) == "" {
			return invalid("a reason and revision instructions are required before ordering")
		}
		r.Stage = PrintRejected
	case "cancel":
		if !CanTransitionPrint(r.Stage, PrintCancelled) || strings.TrimSpace(a.Note) == "" {
			return invalid("a cancellation reason is required")
		}
		if (r.Stage == PrintOrdered || r.Stage == PrintInTransit || r.HasPrinterOrder()) && (a.ConfirmationURL == "" || a.CancellationDisposition == "") {
			return invalid("printer confirmation and goods/payment disposition required")
		}
		r.Stage = PrintCancelled
		if a.ConfirmationURL != "" {
			r.InternalNote += "\nPrinter cancellation: " + a.ConfirmationURL + "\nDisposition: " + a.CancellationDisposition
		}
	case "dispatch":
		if r.Stage != PrintOrdered {
			return invalid("only an ordered run can be dispatched")
		}
		r.Stage = PrintInTransit
		r.Tracking = a.Tracking
		r.DispatchedAt = a.DispatchedAt // Recording the update is not proof of the printer's dispatch time.
		if a.ETA != "" {
			r.ETA = a.ETA
			r.ETASource = "HQ printer dispatch"
		}
	case "update":
		if r.Stage == PrintCancelled || r.Stage == PrintRejected {
			return invalid("closed run")
		}
		if a.Note == "" && a.ETA == "" && a.InternalNote == "" {
			return invalid("add an update")
		}
		notify = a.Note != "" || a.ETA != "" && a.ETA != r.ETA
		if a.Note != "" && strings.TrimSpace(a.ETASource) == "" {
			return invalid("name the source or who confirmed this manual update")
		}
		if a.ETA != "" {
			r.ETA = a.ETA
			r.ETASource = a.ETASource
		}
	case "preferences":
		r.SlackOptIn = a.SlackOptIn
	case "first_shipment":
		if r.Stage == PrintAvailable || r.Stage == PrintRejected || r.Stage == PrintCancelled {
			return invalid("this run is closed to first shipment changes")
		}
		limit := r.Request.Quantity
		if r.ApprovedQuantity > 0 {
			limit = r.ApprovedQuantity
		}
		if r.ShipmentRequestID != nil || a.InitialQuantity < 1 || a.InitialQuantity > limit || a.Address == nil || a.Address.Validate() != nil {
			return invalid("valid first quantity and destination required before release")
		}
		if r.Request.Product == "postcard_4x6" && !PackQuantity("postcard", a.InitialQuantity) {
			return invalid("choose a postcard pack quantity")
		}
		r.DeliveryQuantity = a.InitialQuantity
		r.DeliveryAddress = a.Address
	case "retry_notifications":
	case "resolve_notification":
		if strings.TrimSpace(a.Note) == "" || (a.DeliveryOutcome != "sent" && a.DeliveryOutcome != "not_sent") {
			return invalid("document provider delivery evidence and outcome")
		}
		if !r.NotificationUncertain {
			return invalid("only an uncertain notification can be reconciled")
		}
	case "review_order":
		if r.Stage != PrintApproved && r.Stage != PrintOrdered && r.Stage != PrintInTransit {
			return invalid("review order evidence before receipt")
		}
		if a.Note == "" || a.Specifications == "" || a.ApprovedQuantity < 1 {
			return invalid("document quantity, specifications and the order discrepancy")
		}
		r.ApprovedQuantity = a.ApprovedQuantity
		r.Specifications = a.Specifications
		r.ReviewedExistingOrder = true
		r.Reconcile = false
		notify = true
		if r.ApprovedAt == nil {
			r.ApprovedAt = &now
			r.ApprovedBy = a.ActorID
			r.Revision = 1
		}
	case "review_receipt":
		if (r.Stage != PrintReceived && r.Stage != PrintAvailable) || strings.TrimSpace(a.Note) == "" || strings.TrimSpace(a.ETASource) == "" || !completePrintEvidence(r, now) {
			return invalid("document warehouse confirmation of complete, fresh receipt evidence")
		}
		r.ReceiptReviewDigest = printEvidenceDigest(r.Links)
		r.ReceiptCorrection = false
		r.Reconcile = false
		notify = true
		notice = PrintNoticeReceiptReview
	case "link", "replace_links":
		if r.LegacyReview {
			return invalid("audit the legacy run before linking purchase orders")
		}
		if (r.Stage != PrintApproved && !(r.Stage == PrintOrdered && r.ReviewedExistingOrder)) || a.ConfirmationURL == "" || len(a.Evidence) == 0 {
			return invalid("approve first and link printer confirmation plus exact PO lines")
		}
		if len(r.Links) > 0 && a.Action != "replace_links" {
			return invalid("links already locked; reconcile with HQ")
		}
		if a.Action == "replace_links" {
			if a.Note == "" {
				return invalid("document why the selected PO lines are being corrected")
			}
			if r.Stage == PrintOrdered && !r.ReviewedExistingOrder {
				return invalid("review existing order evidence before correcting links")
			}
		}
		r.SKU = strings.TrimSpace(a.SKU)
		r.ItemID = a.ItemID
		for _, p := range a.Evidence {
			if p.SKU != r.SKU || p.POID <= 0 || p.LineID <= 0 {
				return invalid("exact PO/line/SKU required")
			}
		}
		r.Links = a.Evidence
	case "observe":
		if !a.Auto {
			return invalid("only the configured PO adapter records observations")
		}
		r.LastPollAt = &now
		r.PollError = a.PollError
		if a.PollError != "" {
			break
		}
		links := []PrintPOLink{}
		for _, p := range r.Links {
			links = append(links, p.PrintPOLink)
		}
		approval := PrintApproval{Quantity: r.ApprovedQuantity, SKU: r.SKU, ReviewedExistingOrder: r.ReviewedExistingOrder}
		if r.ApprovedAt != nil {
			approval.At = *r.ApprovedAt
		}
		result := ObservePrintPOs(r.Stage, approval, links, a.Evidence, now, time.Hour)
		r.SourceStale = result.Stale
		if result.Stale {
			// Report the actual copy age without letting an old copy correct
			// the last confirmed business evidence or invalidate an open form.
			for i := range r.Links {
				for _, observed := range a.Evidence {
					if observed.PrintPOLink == r.Links[i].PrintPOLink {
						r.Links[i].SyncedAt, r.Links[i].PolledAt = observed.SyncedAt, observed.PolledAt
					}
				}
			}
			break
		}
		if (prev == PrintReceived || prev == PrintAvailable) && printEvidenceDigest(r.Links) != printEvidenceDigest(a.Evidence) {
			r.ReceiptCorrection = true
			r.ReceiptReviewDigest = ""
		}
		r.Reconcile = result.Reconcile || r.ReceiptCorrection
		r.Links = a.Evidence
		if (prev == PrintReceived || prev == PrintAvailable) && r.ReceiptReviewDigest == printEvidenceDigest(r.Links) && completePrintEvidence(r, now) {
			r.Reconcile = false
		}
		if result.Stage != "" {
			r.Stage = result.Stage
		}
		if !result.Reconcile && (r.ETASource == "" || r.ETASource == "Zenventory PO required-by") && (prev == PrintApproved || prev == PrintOrdered || prev == PrintInTransit) {
			var latest *time.Time
			for _, p := range a.Evidence {
				if p.RequiredBy != nil && (latest == nil || p.RequiredBy.After(*latest)) {
					latest = p.RequiredBy
				}
			}
			date := ""
			if latest != nil {
				date = latest.UTC().Format(time.DateOnly)
			}
			if date != r.ETA {
				notify = true
				r.ETA = date
				r.ETASource = "Zenventory PO required-by"
				notice = PrintNoticePOETA
			}
		}
	case "release":
		if r.Stage != PrintReceived || r.LegacyReview || r.Reconcile || r.SourceStale || r.PollError != "" || r.ItemID == 0 || r.SKU == "" || a.Sellable < 1 {
			return invalid("receipt, reconciled complete listing and confirmed sellable stock required")
		}
		if r.Request.InitialQuantity < 1 && r.Origin != "legacy" {
			return invalid("requester must choose the first shipment before release")
		}
		if r.ApprovedQuantity > 0 && r.Request.InitialQuantity > r.ApprovedQuantity {
			return invalid("first quantity exceeds the approved run")
		}
		if r.Request.InitialQuantity > 0 {
			if r.Request.Address == nil || r.Request.Address.Validate() != nil {
				return invalid("first shipment address is required")
			}
			if a.ReleaseStatus != StatusPending && a.ReleaseStatus != StatusAwaitingPayment {
				return invalid("invalid shipment state")
			}
			if IsInternational(r.Request.Address.Country) && (a.ReleaseStatus != StatusAwaitingPayment || a.ReleaseFee <= 0) {
				return invalid("international postage payment required")
			}
		}
		if !r.ListingReady {
			return invalid("complete listing with matching SKU and images required")
		}
		r.Stage = PrintAvailable
	case "audit_legacy":
		if !r.LegacyReview || a.Note == "" {
			return invalid("document original order/warehouse confirmation and link audit")
		}
		r.SKU = a.SKU
		r.ItemID = a.ItemID
		if r.Stage == PrintReceived && r.ShipmentRequestID != nil {
			if !r.LegacyShipmentMatches {
				return invalid("existing first shipment must belong to this requester and contain the exact SKU")
			}
			if r.LegacyListingVisible {
				r.Stage = PrintAvailable
			}
		}
		r.LegacyReview = false
		if r.Stage == PrintApproved || r.Stage == PrintOrdered {
			if a.ApprovedQuantity < 1 || a.Specifications == "" {
				return invalid("review original quantity, artwork and specifications")
			}
			r.ApprovedQuantity = a.ApprovedQuantity
			r.Specifications = a.Specifications
			r.ApprovedAt = &now
			r.ApprovedBy = a.ActorID
			r.ReviewedExistingOrder = true
			r.Revision = 1
		}
	default:
		return invalid("unknown action")
	}

	if a.InternalNote != "" {
		r.InternalNote += "\n" + a.InternalNote
	}
	if a.Note != "" {
		r.PublicNote = a.Note
	}
	record := a.Action != "observe" || prev != r.Stage || notify || previousReconcile != r.Reconcile || previousEvidence != printEvidenceDigest(r.Links)
	eventSource := "HQ"
	if actor.Automatic {
		eventSource = "warehouse PO copy"
	} else if actor.ID == r.UserID {
		eventSource = "requester"
	}
	eventAction := a.Action
	if a.Action == "observe" && prev == r.Stage && notify {
		eventAction = "eta_update"
	}
	if prev != r.Stage {
		notice = PrintNoticeStage
	}
	send := (prev != r.Stage || notify) && a.Action != "audit_legacy" && !(a.Action == "release" && r.Origin == "legacy" && state.ShipmentRequestID != nil)
	return PrintActionResult{Run: *r, Notify: send, RecordEvent: record, Notice: notice, AppendETA: prev == r.Stage && a.ETA != "", EventSource: eventSource, EventAction: eventAction,
		NotifyFirstShipment: a.Action == "release" && state.ShipmentRequestID == nil && r.Request.InitialQuantity > 0 && a.ReleaseStatus == StatusPending,
		SaveConfirmation:    a.ConfirmationURL != "" && (a.Action == "link" || a.Action == "replace_links" || a.Action == "cancel")}, nil
}

func CheckPrintReorder(stage PrintStage) error {
	if stage == PrintAvailable || stage == PrintRejected || stage == PrintCancelled {
		return nil
	}
	return &PrintActionError{Reason: "finish the current run first"}
}

// PrintPollStages are the only stages PrintRunNeedsPoll can pick; the store
// filters on them in SQL so closed runs aren't scanned every minute.
var PrintPollStages = []PrintStage{PrintApproved, PrintOrdered, PrintInTransit, PrintReceived, PrintAvailable}

func PrintRunNeedsPoll(r PrintRunState, now time.Time) bool {
	if len(r.Links) == 0 {
		return false
	}
	switch r.Stage {
	case PrintApproved, PrintOrdered, PrintInTransit, PrintReceived:
		return true
	case PrintAvailable:
		if r.Reconcile || r.SourceStale || r.PollError != "" || r.LastPollAt == nil || !r.LastPollAt.After(now.Add(-15*time.Minute)) {
			return true
		}
		for _, p := range r.Links {
			if !p.Completed || p.Draft || p.Deleted {
				return true
			}
		}
	}
	return false
}
