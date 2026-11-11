package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/hackclub/merch/internal/swag"
	"github.com/jackc/pgx/v5"
	"sort"
	"strings"
	"time"
)

var ErrPrintConflict = errors.New("print run changed; reload before acting")
var ErrPrintAction = errors.New("invalid print action")

type PrintRunData struct {
	DeliveryQuantity      int           `json:"delivery_quantity"`
	DeliveryAddress       *swag.Address `json:"delivery_address,omitempty"`
	ReviewedExistingOrder bool          `json:"reviewed_existing_order"`
	ApprovedQuantity      int           `json:"approved_quantity"`
	ApprovedAt            *time.Time    `json:"approved_at"`
	ApprovedBy            int64         `json:"approved_by"`
	Specifications        string        `json:"specifications"`
	Revision              int           `json:"revision"`
	SKU                   string        `json:"sku"`
	ItemID                int64         `json:"item_id"`
	PublicNote            string        `json:"public_note"`
	Tracking              string        `json:"tracking"`
	ETA                   string        `json:"eta"`
	ETASource             string        `json:"eta_source"`
	DispatchedAt          *time.Time    `json:"dispatched_at"`
	Reconcile             bool          `json:"reconcile"`
	ReceiptCorrection     bool          `json:"receipt_correction"`
	ReceiptReviewDigest   string        `json:"receipt_review_digest,omitempty"`
	LegacyReview          bool          `json:"legacy_review"`
	LegacyStatus          string        `json:"legacy_status"`
	SlackOptIn            bool          `json:"slack_opt_in"`
	ParentID              int64         `json:"parent_id"`
	ParentVersion         int64         `json:"parent_version"`
	LastPollAt            *time.Time    `json:"last_poll_at"`
	PollError             string        `json:"poll_error"`
}
type PrintRun struct {
	ShipmentStatus swag.Status `json:"shipment_status"`
	Stock          *PrintStock `json:"stock,omitempty"`
	PrintRunData
	ID                int64                  `json:"id"`
	UserID            int64                  `json:"user_id"`
	UserEmail         string                 `json:"user_email,omitempty"`
	UserName          string                 `json:"user_name,omitempty"`
	SourcePrintID     *int64                 `json:"source_print_id"`
	SourceAuthorID    *int64                 `json:"source_author_id"`
	Origin            string                 `json:"origin"`
	Stage             swag.PrintStage        `json:"stage"`
	Version           int64                  `json:"version"`
	Request           PrintRequest           `json:"request"`
	InternalNote      string                 `json:"internal_note,omitempty"`
	ShipmentRequestID *int64                 `json:"shipment_request_id"`
	CreatedAt         time.Time              `json:"created_at"`
	Links             []swag.PrintPOEvidence `json:"links"`
}
type PrintStock struct {
	MaxAgeSeconds     float64    `json:"max_age_seconds"`
	WarehouseSellable *int       `json:"warehouse_sellable"`
	Held              *int       `json:"held"`
	Available         *int       `json:"available"`
	Inbound           *int       `json:"inbound"`
	SyncedAt          *time.Time `json:"synced_at"`
	Visible           bool       `json:"visible"`
}

const printRunSelect = `SELECT r.id,r.user_id,u.email,u.name,r.source_print_id,r.source_author_id,r.origin,r.stage,r.version,r.request,r.data,r.internal_note,r.shipment_request_id,r.created_at,
 coalesce((select jsonb_agg(l.evidence order by l.po_id,l.line_id) from print_run_po_links l where l.run_id=r.id),'[]'),
 coalesce((select status from swag_requests s where s.id=r.shipment_request_id),'')
 FROM print_runs r JOIN users u ON u.id=r.user_id`

func scanPrintRun(row pgx.Row) (*PrintRun, error) {
	var r PrintRun
	var data []byte
	err := row.Scan(&r.ID, &r.UserID, &r.UserEmail, &r.UserName, &r.SourcePrintID, &r.SourceAuthorID, &r.Origin, &r.Stage, &r.Version, &r.Request, &data, &r.InternalNote, &r.ShipmentRequestID, &r.CreatedAt, &r.Links, &r.ShipmentStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(data, &r.PrintRunData)
	if r.DeliveryQuantity > 0 {
		r.Request.InitialQuantity = r.DeliveryQuantity
		r.Request.Address = r.DeliveryAddress
	}
	return &r, err
}
func (s *Store) PrintRun(ctx context.Context, id int64) (*PrintRun, error) {
	return scanPrintRun(s.pool.QueryRow(ctx, printRunSelect+` WHERE r.id=$1`, id))
}
func (s *Store) PrintRuns(ctx context.Context, userID int64) ([]*PrintRun, error) {
	rows, err := s.pool.Query(ctx, printRunSelect+` WHERE ($1=0 OR r.user_id=$1) ORDER BY r.id DESC LIMIT 500`, userID)
	if err != nil {
		return nil, err
	}
	return collect(rows, func(r pgx.Rows) (*PrintRun, error) { return scanPrintRun(r) })
}

func (s *Store) PrintRunsForSources(ctx context.Context, prints, authors []int64) ([]*PrintRun, error) {
	rows, err := s.pool.Query(ctx, printRunSelect+` WHERE r.source_print_id=ANY($1::bigint[]) OR r.source_author_id=ANY($2::bigint[])`, prints, authors)
	if err != nil {
		return nil, err
	}
	return collect(rows, func(row pgx.Rows) (*PrintRun, error) { return scanPrintRun(row) })
}

// Poll fairly across all linked runs, including releases that may later need
// reconciliation. Closed newer requests must not starve old active runs.
func (s *Store) PrintRunsToPoll(ctx context.Context) ([]*PrintRun, error) {
	rows, err := s.pool.Query(ctx, printRunSelect+` WHERE r.stage IN ('approved','ordered','in_transit','received','available') AND EXISTS(SELECT 1 FROM print_run_po_links l WHERE l.run_id=r.id)
 AND (r.stage<>'available' OR r.data->>'reconcile'='true' OR coalesce(r.data->>'poll_error','')<>''
 OR (r.data->>'last_poll_at') IS NULL OR (r.data->>'last_poll_at')::timestamptz<=now()-interval '15 minutes'
 OR EXISTS(SELECT 1 FROM print_run_po_links l WHERE l.run_id=r.id AND (l.evidence->>'completed' IS DISTINCT FROM 'true' OR l.evidence->>'draft'='true' OR l.evidence->>'deleted'='true')))
 ORDER BY r.data->>'last_poll_at' NULLS FIRST,r.id LIMIT 50`)
	if err != nil {
		return nil, err
	}
	return collect(rows, func(row pgx.Rows) (*PrintRun, error) { return scanPrintRun(row) })
}

func (r *PrintRun) HasPrinterOrder() bool {
	for _, p := range r.Links {
		if !p.Draft && !p.Deleted && p.CreatedAt != nil {
			return true
		}
	}
	return false
}

// Poll timestamps change every observation; only business evidence needs HQ
// review after receipt. Sort a copy so link order cannot change its identity.
func printEvidenceDigest(proofs []swag.PrintPOEvidence) string {
	copy := append([]swag.PrintPOEvidence(nil), proofs...)
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

func completePrintEvidence(r *PrintRun, now time.Time) bool {
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

type PrintRunEvent struct {
	Details    map[string]any  `json:"details"`
	ID         int64           `json:"id"`
	Stage      swag.PrintStage `json:"stage"`
	Action     string          `json:"action"`
	ActorID    *int64          `json:"actor_id"`
	At         *time.Time      `json:"at"`
	RecordedAt time.Time       `json:"recorded_at"`
	Note       string          `json:"note"`
	Source     string          `json:"source"`
	Version    int64           `json:"version"`
}

func (s *Store) PrintRunHistory(ctx context.Context, id int64) ([]PrintRunEvent, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,stage,action,actor_id,event_at,recorded_at,note,source,version,details FROM print_run_events WHERE run_id=$1 ORDER BY id DESC`, id)
	if err != nil {
		return nil, err
	}
	return collect(rows, func(row pgx.Rows) (PrintRunEvent, error) {
		var e PrintRunEvent
		err := row.Scan(&e.ID, &e.Stage, &e.Action, &e.ActorID, &e.At, &e.RecordedAt, &e.Note, &e.Source, &e.Version, &e.Details)
		return e, err
	})
}

type PrintAction struct {
	DispatchedAt            *time.Time             `json:"dispatched_at"`
	InitialQuantity         int                    `json:"initial_quantity"`
	Address                 *swag.Address          `json:"address"`
	NotificationID          int64                  `json:"notification_id"`
	DeliveryOutcome         string                 `json:"delivery_outcome"`
	Version                 int64                  `json:"version"`
	Action                  string                 `json:"action"`
	ActorID                 int64                  `json:"-"`
	ApprovedQuantity        int                    `json:"approved_quantity"`
	Specifications          string                 `json:"specifications"`
	SKU                     string                 `json:"sku"`
	ItemID                  int64                  `json:"item_id"`
	Note                    string                 `json:"note"`
	InternalNote            string                 `json:"internal_note"`
	Tracking                string                 `json:"tracking"`
	ETA                     string                 `json:"eta"`
	ETASource               string                 `json:"eta_source"`
	ConfirmationURL         string                 `json:"confirmation_url"`
	CancellationDisposition string                 `json:"cancellation_disposition"`
	Evidence                []swag.PrintPOEvidence `json:"-"`
	PollError               string                 `json:"-"`
	Auto                    bool                   `json:"-"`
	Now                     time.Time              `json:"-"`
	SlackOptIn              bool                   `json:"slack_opt_in"`
	ParentID                int64                  `json:"parent_id"`
	ReleaseStatus           swag.Status            `json:"-"`
	ReleaseFee              int                    `json:"-"`
	Sellable                int                    `json:"-"`
}

func (s *Store) ActPrintRun(ctx context.Context, id int64, a PrintAction) (*PrintRun, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	r, err := scanPrintRun(tx.QueryRow(ctx, printRunSelect+` WHERE r.id=$1 FOR UPDATE OF r`, id))
	if err != nil {
		return nil, err
	}
	// Release retries return the existing parcel even if the caller has an old version.
	if a.Action == "release" && r.Stage == swag.PrintAvailable && r.ShipmentRequestID != nil {
		return r, nil
	}
	if r.Version != a.Version {
		return nil, ErrPrintConflict
	}
	now := a.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	prev := r.Stage
	previousReconcile := r.Reconcile
	previousEvidence := printEvidenceDigest(r.Links)
	hadShipment := r.ShipmentRequestID != nil
	notify := false
	updateMessage := ""
	invalid := func(msg string) (*PrintRun, error) { return nil, fmt.Errorf("%w: %s", ErrPrintAction, msg) }
	switch a.Action {
	case "approve":
		if r.Stage != swag.PrintSubmitted || (r.Origin == "legacy" && r.LegacyStatus == "printing") || a.ApprovedQuantity < 1 || strings.TrimSpace(a.Specifications) == "" {
			return invalid("review artwork, specifications, quantity and budget first")
		}
		r.Stage = swag.PrintApproved
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
		if (r.Stage != swag.PrintSubmitted && r.Stage != swag.PrintApproved) || strings.TrimSpace(a.Note) == "" {
			return invalid("a reason and revision instructions are required before ordering")
		}
		r.Stage = swag.PrintRejected
	case "cancel":
		if !swag.CanTransitionPrint(r.Stage, swag.PrintCancelled) || strings.TrimSpace(a.Note) == "" {
			return invalid("a cancellation reason is required")
		}
		if (r.Stage == swag.PrintOrdered || r.Stage == swag.PrintInTransit || r.HasPrinterOrder()) && (a.ConfirmationURL == "" || a.CancellationDisposition == "") {
			return invalid("printer confirmation and goods/payment disposition required")
		}
		r.Stage = swag.PrintCancelled
		if a.ConfirmationURL != "" {
			r.InternalNote += "\nPrinter cancellation: " + a.ConfirmationURL + "\nDisposition: " + a.CancellationDisposition
		}
	case "dispatch":
		if r.Stage != swag.PrintOrdered {
			return invalid("only an ordered run can be dispatched")
		}
		r.Stage = swag.PrintInTransit
		r.Tracking = a.Tracking
		r.DispatchedAt = a.DispatchedAt // Recording the update is not proof of the printer's dispatch time.
		if a.ETA != "" {
			r.ETA = a.ETA
			r.ETASource = "HQ printer dispatch"
		}
	case "update":
		if r.Stage == swag.PrintCancelled || r.Stage == swag.PrintRejected {
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
		if !a.SlackOptIn {
			_, err = tx.Exec(ctx, `update print_notifications set status='cancelled' where run_id=$1 and channel='slack' and status in ('queued','failed')`, id)
			if err != nil {
				return nil, err
			}
		}
	case "first_shipment":
		if r.Stage == swag.PrintAvailable || r.Stage == swag.PrintRejected || r.Stage == swag.PrintCancelled {
			return invalid("this run is closed to first shipment changes")
		}
		limit := r.Request.Quantity
		if r.ApprovedQuantity > 0 {
			limit = r.ApprovedQuantity
		}
		if r.ShipmentRequestID != nil || a.InitialQuantity < 1 || a.InitialQuantity > limit || a.Address == nil || a.Address.Validate() != nil {
			return invalid("valid first quantity and destination required before release")
		}
		if r.Request.Product == "postcard_4x6" && !swag.PackQuantity("postcard", a.InitialQuantity) {
			return invalid("choose a postcard pack quantity")
		}
		r.DeliveryQuantity = a.InitialQuantity
		r.DeliveryAddress = a.Address
	case "retry_notifications":
		_, err = tx.Exec(ctx, `update print_notifications set next_attempt_at=now() where run_id=$1 and status='failed'`, id)
		if err != nil {
			return nil, err
		}
	case "resolve_notification":
		if strings.TrimSpace(a.Note) == "" || (a.DeliveryOutcome != "sent" && a.DeliveryOutcome != "not_sent") {
			return invalid("document provider delivery evidence and outcome")
		}
		status := "sent"
		if a.DeliveryOutcome == "not_sent" {
			status = "queued"
		}
		tag, e := tx.Exec(ctx, `update print_notifications set status=$3,first_attempt_at=null,next_attempt_at=now(),error='' where id=$1 and run_id=$2 and status='uncertain'`, a.NotificationID, id, status)
		if e != nil {
			return nil, e
		}
		if tag.RowsAffected() != 1 {
			return invalid("only an uncertain notification can be reconciled")
		}
	case "review_order":
		if r.Stage != swag.PrintApproved && r.Stage != swag.PrintOrdered && r.Stage != swag.PrintInTransit {
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
		if (r.Stage != swag.PrintReceived && r.Stage != swag.PrintAvailable) || strings.TrimSpace(a.Note) == "" || strings.TrimSpace(a.ETASource) == "" || !completePrintEvidence(r, now) {
			return invalid("document warehouse confirmation of complete, fresh receipt evidence")
		}
		r.ReceiptReviewDigest = printEvidenceDigest(r.Links)
		r.ReceiptCorrection = false
		r.Reconcile = false
		notify = true
		updateMessage = a.Note + " Source: " + a.ETASource + ". Received quantity unavailable."
	case "link", "replace_links":
		if r.LegacyReview {
			return invalid("audit the legacy run before linking purchase orders")
		}
		if (r.Stage != swag.PrintApproved && !(r.Stage == swag.PrintOrdered && r.ReviewedExistingOrder)) || a.ConfirmationURL == "" || len(a.Evidence) == 0 {
			return invalid("approve first and link printer confirmation plus exact PO lines")
		}
		if len(r.Links) > 0 && a.Action != "replace_links" {
			return invalid("links already locked; reconcile with HQ")
		}
		if a.Action == "replace_links" {
			if a.Note == "" {
				return invalid("document why the selected PO lines are being corrected")
			}
			if r.Stage == swag.PrintOrdered && !r.ReviewedExistingOrder {
				return invalid("review existing order evidence before correcting links")
			}
			if _, err = tx.Exec(ctx, `delete from print_run_po_links where run_id=$1`, id); err != nil {
				return nil, err
			}
		}
		r.SKU = strings.TrimSpace(a.SKU)
		r.ItemID = a.ItemID
		for _, p := range a.Evidence {
			if p.SKU != r.SKU || p.POID <= 0 || p.LineID <= 0 {
				return invalid("exact PO/line/SKU required")
			}
			_, err = tx.Exec(ctx, `INSERT INTO print_run_po_links(run_id,po_id,line_id,sku,evidence,confirmation_url,revision) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, p.POID, p.LineID, p.SKU, p, a.ConfirmationURL, r.Revision)
			if err != nil {
				return nil, mapErr(err)
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
		for _, p := range a.Evidence {
			_, err = tx.Exec(ctx, `INSERT INTO print_po_observations(run_id,po_id,line_id,synced_at,polled_at,evidence) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, id, p.POID, p.LineID, p.SyncedAt, p.PolledAt, p)
			if err != nil {
				return nil, err
			}
			_, err = tx.Exec(ctx, `UPDATE print_run_po_links SET evidence=$4 WHERE run_id=$1 AND po_id=$2 AND line_id=$3 AND sku=$5`, id, p.POID, p.LineID, p, p.SKU)
			if err != nil {
				return nil, err
			}
		}
		links := []swag.PrintPOLink{}
		for _, p := range r.Links {
			links = append(links, p.PrintPOLink)
		}
		approval := swag.PrintApproval{Quantity: r.ApprovedQuantity, SKU: r.SKU}
		approval.ReviewedExistingOrder = r.ReviewedExistingOrder
		if r.ApprovedAt != nil {
			approval.At = *r.ApprovedAt
		}
		result := swag.ObservePrintPOs(r.Stage, approval, links, a.Evidence, now, time.Hour)
		if (prev == swag.PrintReceived || prev == swag.PrintAvailable) && printEvidenceDigest(r.Links) != printEvidenceDigest(a.Evidence) {
			r.ReceiptCorrection = true
			r.ReceiptReviewDigest = ""
		}
		r.Reconcile = result.Reconcile || r.ReceiptCorrection
		r.Links = a.Evidence
		if (prev == swag.PrintReceived || prev == swag.PrintAvailable) && r.ReceiptReviewDigest == printEvidenceDigest(r.Links) && completePrintEvidence(r, now) {
			r.Reconcile = false
		}
		if result.Stage != "" {
			r.Stage = result.Stage
		}
		if !result.Reconcile && (r.ETASource == "" || r.ETASource == "Zenventory PO required-by") && (prev == swag.PrintApproved || prev == swag.PrintOrdered || prev == swag.PrintInTransit) {
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
				if date == "" {
					date = "not confirmed"
				}
				updateMessage = "Expected warehouse date for your print run is now " + date + " (linked PO required-by). This is an estimate, not a delivery promise."
			}
		}
	case "release":
		if r.Stage != swag.PrintReceived || r.LegacyReview || r.Reconcile || r.PollError != "" || r.ItemID == 0 || r.SKU == "" || a.Sellable < 1 {
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
			if a.ReleaseStatus != swag.StatusPending && a.ReleaseStatus != swag.StatusAwaitingPayment {
				return invalid("invalid shipment state")
			}
			if swag.IsInternational(r.Request.Address.Country) && (a.ReleaseStatus != swag.StatusAwaitingPayment || a.ReleaseFee <= 0) {
				return invalid("international postage payment required")
			}
		}
		tag, err := tx.Exec(ctx, `UPDATE items SET visible=true,updated_at=now() WHERE id=$1 AND sku=$2 AND name<>'' AND cardinality(images)>0`, r.ItemID, r.SKU)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() != 1 {
			return invalid("complete listing with matching SKU and images required")
		}
		if r.ShipmentRequestID == nil && r.Request.InitialQuantity > 0 {
			ad := r.Request.Address
			var rid int64
			err = tx.QueryRow(ctx, `INSERT INTO swag_requests(user_id,status,purpose,first_name,last_name,line_1,line_2,city,state,postal_code,country,phone_number,shipping_fee_cents,note) VALUES($1,$2,'staff',$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING id`, r.UserID, a.ReleaseStatus, ad.FirstName, ad.LastName, ad.Line1, ad.Line2, ad.City, ad.State, ad.PostalCode, ad.Country, ad.Phone, a.ReleaseFee, fmt.Sprintf("First shipment from print run #%d", id)).Scan(&rid)
			if err != nil {
				return nil, err
			}
			_, err = tx.Exec(ctx, `INSERT INTO swag_request_items(request_id,item_id,quantity,sku,variant) VALUES($1,$2,$3,$4,'')`, rid, r.ItemID, r.Request.InitialQuantity, r.SKU)
			if err != nil {
				return nil, err
			}
			r.ShipmentRequestID = &rid
			r.ShipmentStatus = a.ReleaseStatus
		}
		r.Stage = swag.PrintAvailable
	case "audit_legacy":
		if !r.LegacyReview || a.Note == "" {
			return invalid("document original order/warehouse confirmation and link audit")
		}
		r.SKU = a.SKU
		r.ItemID = a.ItemID
		if r.Stage == swag.PrintReceived && r.ShipmentRequestID != nil {
			var matching bool
			err = tx.QueryRow(ctx, `select exists(select 1 from swag_requests s join swag_request_items i on i.request_id=s.id where s.id=$1 and s.user_id=$2 and i.sku=$3 and i.quantity>0)`, *r.ShipmentRequestID, r.UserID, r.SKU).Scan(&matching)
			if err != nil {
				return nil, err
			}
			if !matching {
				return invalid("existing first shipment must belong to this requester and contain the exact SKU")
			}
			var visible bool
			err = tx.QueryRow(ctx, `select visible AND name<>'' AND cardinality(images)>0 from items where id=$1 and sku=$2`, r.ItemID, r.SKU).Scan(&visible)
			if err != nil {
				return nil, err
			}
			if visible {
				r.Stage = swag.PrintAvailable
			}
		}
		r.LegacyReview = false
		if r.Stage == swag.PrintApproved || r.Stage == swag.PrintOrdered {
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
	if a.ConfirmationURL != "" && (a.Action == "link" || a.Action == "replace_links" || a.Action == "cancel") {
		_, err = tx.Exec(ctx, `insert into print_printer_confirmations(run_id,version,url,actor_id) values($1,$2,$3,$4)`, id, r.Version+1, a.ConfirmationURL, a.ActorID)
		if err != nil {
			return nil, err
		}
	}
	if a.Note != "" {
		r.PublicNote = a.Note
	}
	recordEvent := a.Action != "observe" || prev != r.Stage || notify || previousReconcile != r.Reconcile || previousEvidence != printEvidenceDigest(r.Links)
	if recordEvent {
		r.Version++
	}
	_, err = tx.Exec(ctx, `UPDATE print_runs SET stage=$2,version=$3,data=$4,internal_note=$5,shipment_request_id=$6 WHERE id=$1`, id, r.Stage, r.Version, r.PrintRunData, r.InternalNote, r.ShipmentRequestID)
	if err != nil {
		return nil, err
	}
	// Poll metadata alone does not fabricate timeline activity or notifications.
	if recordEvent {
		var eid int64
		var actor *int64
		if a.ActorID != 0 {
			actor = &a.ActorID
		}
		source := "HQ"
		if a.Auto {
			source = "warehouse PO copy"
		} else if a.ActorID == r.UserID {
			source = "requester"
		}
		details := map[string]any{"from_stage": prev, "stage_changed": prev != r.Stage, "approved_quantity": r.ApprovedQuantity, "specifications": r.Specifications, "art_revision": r.Revision, "sku": r.SKU, "item_id": r.ItemID, "update_source": a.ETASource, "po_links": r.Links}
		eventAction := a.Action
		if a.Action == "observe" && prev == r.Stage && notify {
			eventAction = "eta_update"
		}
		err = tx.QueryRow(ctx, `INSERT INTO print_run_events(run_id,stage,action,actor_id,event_at,note,source,version,details) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`, id, r.Stage, eventAction, actor, now, a.Note, source, r.Version, details).Scan(&eid)
		if err != nil {
			return nil, err
		}
		if (prev != r.Stage || notify) && a.Action != "audit_legacy" && !(a.Action == "release" && r.Origin == "legacy" && hadShipment) {
			body := printStageMessage(r, a)
			if prev == r.Stage {
				body = a.Note
				if updateMessage != "" {
					body = updateMessage
				}
				if a.ETA != "" {
					body += "\nExpected warehouse arrival: " + a.ETA + " (" + a.ETASource + "). This is not a delivery promise."
				}
			}
			err = queuePrintNotice(ctx, tx, r, eid, body)
			if err != nil {
				return nil, err
			}
		}
		if a.Action == "release" && !hadShipment && r.ShipmentRequestID != nil && r.ShipmentStatus == swag.StatusPending {
			_, err = tx.Exec(ctx, `insert into print_notifications(run_id,event_id,channel,request_id,destination,subject,body) values($1,$2,'shipment_email',$3,$4,'','')`, r.ID, eid, *r.ShipmentRequestID, r.UserEmail)
			if err != nil {
				return nil, err
			}
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.PrintRun(ctx, id)
}

// ReorderPrintRun uses the parent's submitted version as a retry key. A fresh
// intentional reorder reloads the new parent version, giving a distinct run.
func (s *Store) ReorderPrintRun(ctx context.Context, id, version int64) (*PrintRun, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	p, err := scanPrintRun(tx.QueryRow(ctx, printRunSelect+` WHERE r.id=$1 FOR UPDATE OF r`, id))
	if err != nil {
		return nil, err
	}
	var existing int64
	err = tx.QueryRow(ctx, `select id from print_runs where data->>'parent_id'=$1 and data->>'parent_version'=$2 limit 1`, fmt.Sprint(id), fmt.Sprint(version)).Scan(&existing)
	if err == nil {
		return s.PrintRun(ctx, existing)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if p.Version != version {
		return nil, ErrPrintConflict
	}
	if p.Stage != swag.PrintAvailable && p.Stage != swag.PrintRejected && p.Stage != swag.PrintCancelled {
		return nil, ErrPrintAction
	}
	r := p.Request
	var source, idNew int64
	err = tx.QueryRow(ctx, `insert into print_requests(user_id,product,title,quantity,front_url,back_url,note,initial_quantity,address,front_preview_url,back_preview_url,landscape,program,deadline,finish) values($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) returning id`, p.UserID, r.Product, r.Title, r.Quantity, r.FrontURL, r.BackURL, r.Note, r.InitialQuantity, r.Address, r.FrontPreviewURL, r.BackPreviewURL, r.Landscape, r.Program, r.Deadline, r.Finish).Scan(&source)
	if err != nil {
		return nil, err
	}
	origin := p.Origin
	if origin == "legacy" {
		origin = "staff"
		if p.SourceAuthorID != nil {
			origin = "author"
		}
	}
	err = tx.QueryRow(ctx, `update print_runs set data=jsonb_build_object('parent_id',$2::bigint,'parent_version',$3::bigint,'sku',$5::text,'item_id',$6::bigint),origin=$4 where source_print_id=$1 returning id`, source, id, version, origin, p.SKU, p.ItemID).Scan(&idNew)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `update print_runs set version=version+1 where id=$1`, id)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `insert into print_run_events(run_id,stage,action,event_at,note,source,version) values($1,$2,'reorder',now(),$3,'requester',$4)`, id, p.Stage, fmt.Sprintf("New print run #%d", idNew), version+1)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.PrintRun(ctx, idNew)
}

func printStageMessage(r *PrintRun, a PrintAction) string {
	item := r.Request.Title
	eta := r.ETA
	if eta == "" {
		eta = "not confirmed"
	}
	switch r.Stage {
	case swag.PrintApproved:
		return fmt.Sprintf("Your %s request is approved for %d. HQ will place the printer order next.", item, r.ApprovedQuantity)
	case swag.PrintOrdered:
		return fmt.Sprintf("Your linked purchase order confirms %d %s ordered. Expected warehouse date: %s. We will tell you when the printer ships.", r.ApprovedQuantity, item, eta)
	case swag.PrintInTransit:
		return "Your print run has shipped from the printer to the warehouse. Estimated arrival: " + eta + ". This is not your first parcel."
	case swag.PrintReceived:
		return "The warehouse marked your linked purchase order complete. Your print run is received at the warehouse. The shop listing awaits release. Your first parcel has not shipped."
	case swag.PrintAvailable:
		if r.ShipmentRequestID != nil {
			state := "awaiting review"
			if a.ReleaseStatus == swag.StatusAwaitingPayment {
				state = "awaiting postage payment"
			}
			return fmt.Sprintf("%s is available in the shop. Your first %d are in shipment request #%d, %s.", item, r.Request.InitialQuantity, *r.ShipmentRequestID, state)
		}
		return item + " is available in the shop. No first shipment was requested."
	case swag.PrintRejected:
		return "Your " + item + " request was rejected: " + a.Note + " No order was placed."
	case swag.PrintCancelled:
		detail := "No order was placed."
		if a.ConfirmationURL != "" {
			detail = "Printer confirmed cancellation."
		}
		return "Your " + item + " print request was cancelled. " + a.Note + " " + detail
	}
	return a.Note
}
func queuePrintNotice(ctx context.Context, tx pgx.Tx, r *PrintRun, eid int64, body string) error {
	if body == "" {
		return nil
	}
	subject := fmt.Sprintf("Print #%d · %s", r.ID, printStageLabel(r.Stage))
	_, err := tx.Exec(ctx, `INSERT INTO print_notifications(run_id,event_id,channel,destination,subject,body) VALUES($1,$2,'email',$3,$4,$5)`, r.ID, eid, r.UserEmail, subject, body)
	if err != nil {
		return err
	}
	if r.SlackOptIn {
		// Free-text HQ updates can contain invoice or address details. Slack
		// receives stage facts and a link; full private updates stay in email.
		slackBody := fmt.Sprintf("Print request #%d: %s. Requested %d; approved %d. Printing status is separate from your first parcel.", r.ID, printStageLabel(r.Stage), r.Request.Quantity, r.ApprovedQuantity)
		if _, err := time.Parse(time.DateOnly, r.ETA); err == nil {
			slackBody += " Expected warehouse date: " + r.ETA + " (estimate)."
		}
		if r.ShipmentRequestID != nil {
			slackBody += fmt.Sprintf(" First shipment request #%d: %s.", *r.ShipmentRequestID, r.ShipmentStatus)
		}
		_, err = tx.Exec(ctx, `INSERT INTO print_notifications(run_id,event_id,channel,destination,subject,body) SELECT $1,$2,'slack',slack_id,$3,$4 FROM users WHERE id=$5 AND slack_id<>''`, r.ID, eid, subject, slackBody, r.UserID)
	}
	return err
}

type PrintNotification struct {
	Preview        string `json:"preview"`
	RequestID      *int64 `json:"request_id,omitempty"`
	IdempotencyKey string `json:"-"`
	ID             int64  `json:"id"`
	RunID          int64  `json:"run_id"`
	EventID        int64  `json:"event_id"`
	Channel        string `json:"channel"`
	Destination    string `json:"-"`
	Subject        string `json:"subject"`
	Body           string `json:"body"`
	Status         string `json:"status"`
	Attempts       int    `json:"attempts"`
	Error          string `json:"error,omitempty"`
}

func (s *Store) PrintNotifications(ctx context.Context, id int64) ([]PrintNotification, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,run_id,event_id,case when channel='shipment_email' then 'email' else channel end,subject,body,status,attempts,error,request_id FROM print_notifications WHERE run_id=$1 ORDER BY id DESC`, id)
	if err != nil {
		return nil, err
	}
	return collect(rows, func(row pgx.Rows) (PrintNotification, error) {
		var n PrintNotification
		err := row.Scan(&n.ID, &n.RunID, &n.EventID, &n.Channel, &n.Subject, &n.Body, &n.Status, &n.Attempts, &n.Error, &n.RequestID)
		return n, err
	})
}

type PrintConfirmation struct {
	Version int64     `json:"version"`
	URL     string    `json:"url"`
	ActorID *int64    `json:"actor_id"`
	At      time.Time `json:"at"`
}

func (s *Store) PrintConfirmations(ctx context.Context, id int64) ([]PrintConfirmation, error) {
	rows, err := s.pool.Query(ctx, `select version,url,actor_id,created_at from print_printer_confirmations where run_id=$1 order by version desc`, id)
	if err != nil {
		return nil, err
	}
	return collect(rows, func(row pgx.Rows) (PrintConfirmation, error) {
		var c PrintConfirmation
		err := row.Scan(&c.Version, &c.URL, &c.ActorID, &c.At)
		return c, err
	})
}

// A transaction lease serializes workers. Transport retries use the same key.
// After 23h an ambiguous failure needs human reconciliation because Loops' key
// expires at 24h; we never blindly resend outside its deduplication window.
func (s *Store) PreparePrintNotificationLinks(ctx context.Context, baseURL string) error {
	_, err := s.pool.Exec(ctx, `UPDATE print_notifications SET
 body=CASE WHEN position('View print request: ' in body)=0 THEN body||E'\n\nView print request: '||$1||'/print-requests/'||run_id::text ELSE body END,
 idempotency_key=CASE WHEN idempotency_key='' THEN 'merch-print-'||md5($1||':'||event_id::text||':'||channel) ELSE idempotency_key END
	 WHERE channel IN ('email','slack') AND status='queued' AND first_attempt_at IS NULL AND (idempotency_key='' OR position('View print request: ' in body)=0)`, strings.TrimRight(baseURL, "/"))
	return err
}
func (s *Store) DeliverPrintNotifications(ctx context.Context, channel string, send func(PrintNotification) error) error {
	for i := 0; i < 20; i++ {
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return err
		}
		var n PrintNotification
		var first *time.Time
		err = tx.QueryRow(ctx, `SELECT id,run_id,event_id,channel,destination,subject,body,status,attempts,first_attempt_at,idempotency_key,request_id,preview FROM print_notifications WHERE channel=$1 AND body<>'' AND status IN ('queued','failed','sending') AND next_attempt_at<=now() ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 1`, channel).Scan(&n.ID, &n.RunID, &n.EventID, &n.Channel, &n.Destination, &n.Subject, &n.Body, &n.Status, &n.Attempts, &first, &n.IdempotencyKey, &n.RequestID, &n.Preview)
		if errors.Is(err, pgx.ErrNoRows) {
			tx.Rollback(ctx)
			return nil
		}
		if err != nil {
			tx.Rollback(ctx)
			return err
		}
		if first != nil && time.Since(*first) > 23*time.Hour {
			_, err = tx.Exec(ctx, `UPDATE print_notifications SET status='uncertain',error='Transport deduplication window expired; reconcile delivery before retrying.' WHERE id=$1`, n.ID)
			if err != nil {
				tx.Rollback(ctx)
				return err
			}
			if err = tx.Commit(ctx); err != nil {
				return err
			}
			continue
		}
		n.Attempts++
		if n.IdempotencyKey == "" {
			n.IdempotencyKey = fmt.Sprintf("print-event-%d-%s", n.EventID, n.Channel)
		}
		_, err = tx.Exec(ctx, `UPDATE print_notifications SET status='sending',first_attempt_at=coalesce(first_attempt_at,now()),attempts=$2,next_attempt_at=now()+interval '5 minutes',idempotency_key=$3 WHERE id=$1`, n.ID, n.Attempts, n.IdempotencyKey)
		if err != nil {
			tx.Rollback(ctx)
			return err
		}
		if err = tx.Commit(ctx); err != nil {
			return err
		}
		status, errText := "sent", ""
		if err = send(n); err != nil {
			status = "failed"
			errText = err.Error()
		}
		tx, err = s.pool.Begin(ctx)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE print_notifications SET status=$2,error=$3,next_attempt_at=now()+interval '1 minute'*least(60,power(2,least(attempts,6))) WHERE id=$1 AND attempts=$4 AND status='sending'`, n.ID, status, errText, n.Attempts)
		if err == nil && (channel == "email" || channel == "shipment_email") && tag.RowsAffected() == 1 {
			_, err = tx.Exec(ctx, `INSERT INTO sent_emails(kind,user_id,to_email,subject,preview,body,idempotency_key,status,error,release,print_run_id,print_event_id,request_id) SELECT CASE WHEN $9='shipment_email' THEN 'received' ELSE 'print_'||e.stage END,r.user_id,$2,$3,coalesce(nullif($11,''),$3),$4,$5,$6,$7,'',r.id,$8,$10 FROM print_runs r JOIN print_run_events e ON e.id=$8 WHERE r.id=$1`, n.RunID, n.Destination, n.Subject, n.Body, n.IdempotencyKey, status, errText, n.EventID, channel, n.RequestID, n.Preview)
		}
		if err != nil {
			tx.Rollback(ctx)
			return err
		}
		if err = tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}
