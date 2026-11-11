package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/hackclub/merch/internal/swag"
	"github.com/jackc/pgx/v5"
	"strings"
	"time"
)

var ErrPrintConflict = errors.New("print run changed; reload before acting")
var ErrPrintAction = errors.New("invalid print action")

type PrintRunData struct {
	ApprovedQuantity int        `json:"approved_quantity"`
	ApprovedAt       *time.Time `json:"approved_at"`
	ApprovedBy       int64      `json:"approved_by"`
	Specifications   string     `json:"specifications"`
	Revision         int        `json:"revision"`
	SKU              string     `json:"sku"`
	ItemID           int64      `json:"item_id"`
	PublicNote       string     `json:"public_note"`
	Tracking         string     `json:"tracking"`
	ETA              string     `json:"eta"`
	ETASource        string     `json:"eta_source"`
	DispatchedAt     *time.Time `json:"dispatched_at"`
	Reconcile        bool       `json:"reconcile"`
	LegacyReview     bool       `json:"legacy_review"`
	LegacyStatus     string     `json:"legacy_status"`
	SlackOptIn       bool       `json:"slack_opt_in"`
	ParentID         int64      `json:"parent_id"`
	LastPollAt       *time.Time `json:"last_poll_at"`
	PollError        string     `json:"poll_error"`
}
type PrintRun struct {
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

const printRunSelect = `SELECT r.id,r.user_id,u.email,u.name,r.source_print_id,r.source_author_id,r.origin,r.stage,r.version,r.request,r.data,r.internal_note,r.shipment_request_id,r.created_at,
 coalesce((select jsonb_agg(l.evidence order by l.po_id,l.line_id) from print_run_po_links l where l.run_id=r.id),'[]')
 FROM print_runs r JOIN users u ON u.id=r.user_id`

func scanPrintRun(row pgx.Row) (*PrintRun, error) {
	var r PrintRun
	var data []byte
	err := row.Scan(&r.ID, &r.UserID, &r.UserEmail, &r.UserName, &r.SourcePrintID, &r.SourceAuthorID, &r.Origin, &r.Stage, &r.Version, &r.Request, &data, &r.InternalNote, &r.ShipmentRequestID, &r.CreatedAt, &r.Links)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(data, &r.PrintRunData)
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

type PrintRunEvent struct {
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
	rows, err := s.pool.Query(ctx, `SELECT id,stage,action,actor_id,event_at,recorded_at,note,source,version FROM print_run_events WHERE run_id=$1 ORDER BY id DESC`, id)
	if err != nil {
		return nil, err
	}
	return collect(rows, func(row pgx.Rows) (PrintRunEvent, error) {
		var e PrintRunEvent
		err := row.Scan(&e.ID, &e.Stage, &e.Action, &e.ActorID, &e.At, &e.RecordedAt, &e.Note, &e.Source, &e.Version)
		return e, err
	})
}

type PrintAction struct {
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
	notify := false
	invalid := func(msg string) (*PrintRun, error) { return nil, fmt.Errorf("%w: %s", ErrPrintAction, msg) }
	switch a.Action {
	case "approve":
		if r.Stage != swag.PrintSubmitted || a.ApprovedQuantity < 1 || strings.TrimSpace(a.Specifications) == "" {
			return invalid("review artwork, specifications, quantity and budget first")
		}
		r.Stage = swag.PrintApproved
		r.ApprovedQuantity = a.ApprovedQuantity
		r.Specifications = a.Specifications
		r.ApprovedAt = &now
		r.ApprovedBy = a.ActorID
		r.Revision = 1
		r.SKU = strings.TrimSpace(a.SKU)
	case "reject":
		if (r.Stage != swag.PrintSubmitted && r.Stage != swag.PrintApproved) || strings.TrimSpace(a.Note) == "" {
			return invalid("a reason and revision instructions are required before ordering")
		}
		r.Stage = swag.PrintRejected
	case "cancel":
		if !swag.CanTransitionPrint(r.Stage, swag.PrintCancelled) || strings.TrimSpace(a.Note) == "" {
			return invalid("a cancellation reason is required")
		}
		if (r.Stage == swag.PrintOrdered || r.Stage == swag.PrintInTransit) && (a.ConfirmationURL == "" || a.CancellationDisposition == "") {
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
		r.DispatchedAt = &now
		r.ETA = a.ETA
		r.ETASource = "HQ printer dispatch"
	case "update":
		if r.Stage == swag.PrintCancelled || r.Stage == swag.PrintRejected {
			return invalid("closed run")
		}
		if a.Note == "" && a.ETA == "" && a.InternalNote == "" {
			return invalid("add an update")
		}
		notify = a.Note != "" || a.ETA != "" && a.ETA != r.ETA
		r.ETA = a.ETA
		r.ETASource = a.ETASource
	case "preferences":
		r.SlackOptIn = a.SlackOptIn
	case "link":
		if r.Stage != swag.PrintApproved || a.ConfirmationURL == "" || len(a.Evidence) == 0 {
			return invalid("approve first and link printer confirmation plus exact PO lines")
		}
		if len(r.Links) > 0 {
			return invalid("links already locked; reconcile with HQ")
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
			r.Reconcile = true
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
		if r.ApprovedAt != nil {
			approval.At = *r.ApprovedAt
		}
		result := swag.ObservePrintPOs(r.Stage, approval, links, a.Evidence, now, time.Hour)
		r.Reconcile = result.Reconcile
		r.Links = a.Evidence
		if result.Stage != "" {
			r.Stage = result.Stage
		}
	case "release":
		if r.Stage != swag.PrintReceived || r.LegacyReview || r.Reconcile || r.ItemID == 0 || r.SKU == "" || a.Sellable < 1 {
			return invalid("receipt, reconciled complete listing and confirmed sellable stock required")
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
		}
		r.Stage = swag.PrintAvailable
	case "audit_legacy":
		if !r.LegacyReview || a.Note == "" {
			return invalid("document original order/warehouse confirmation and link audit")
		}
		r.LegacyReview = false
		r.SKU = a.SKU
		r.ItemID = a.ItemID
		if r.Stage == swag.PrintReceived && r.ShipmentRequestID != nil {
			var visible bool
			err = tx.QueryRow(ctx, `select visible from items where id=$1 and sku=$2`, r.ItemID, r.SKU).Scan(&visible)
			if err != nil {
				return nil, err
			}
			if visible {
				r.Stage = swag.PrintAvailable
			}
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
	r.Version++
	_, err = tx.Exec(ctx, `UPDATE print_runs SET stage=$2,version=$3,data=$4,internal_note=$5,shipment_request_id=$6 WHERE id=$1`, id, r.Stage, r.Version, r.PrintRunData, r.InternalNote, r.ShipmentRequestID)
	if err != nil {
		return nil, err
	}
	// Poll metadata alone does not fabricate timeline activity or notifications.
	if a.Action != "observe" || prev != r.Stage {
		var eid int64
		var actor *int64
		if a.ActorID != 0 {
			actor = &a.ActorID
		}
		source := "HQ"
		if a.Auto {
			source = "warehouse PO copy"
		}
		err = tx.QueryRow(ctx, `INSERT INTO print_run_events(run_id,stage,action,actor_id,event_at,note,source,version) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, id, r.Stage, a.Action, actor, now, a.Note, source, r.Version).Scan(&eid)
		if err != nil {
			return nil, err
		}
		if prev != r.Stage || notify {
			body := printStageMessage(r, a)
			err = queuePrintNotice(ctx, tx, r, eid, body)
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
	err = tx.QueryRow(ctx, `update print_runs set data=jsonb_build_object('parent_id',$2::bigint,'parent_version',$3::bigint),origin=$4 where source_print_id=$1 returning id`, source, id, version, p.Origin).Scan(&idNew)
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
	subject := fmt.Sprintf("Print #%d · %s", r.ID, r.Stage)
	_, err := tx.Exec(ctx, `INSERT INTO print_notifications(run_id,event_id,channel,destination,subject,body) VALUES($1,$2,'email',$3,$4,$5)`, r.ID, eid, r.UserEmail, subject, body)
	if err != nil {
		return err
	}
	if r.SlackOptIn {
		// Free-text HQ updates can contain invoice or address details. Slack
		// receives stage facts and a link; full private updates stay in email.
		slackBody := fmt.Sprintf("Print request #%d: %s. Requested %d; approved %d. Printing status is separate from your first parcel.", r.ID, r.Stage, r.Request.Quantity, r.ApprovedQuantity)
		_, err = tx.Exec(ctx, `INSERT INTO print_notifications(run_id,event_id,channel,destination,subject,body) SELECT $1,$2,'slack',slack_id,$3,$4 FROM users WHERE id=$5 AND slack_id<>''`, r.ID, eid, subject, slackBody, r.UserID)
	}
	return err
}

type PrintNotification struct {
	ID          int64  `json:"id"`
	RunID       int64  `json:"run_id"`
	EventID     int64  `json:"event_id"`
	Channel     string `json:"channel"`
	Destination string `json:"-"`
	Subject     string `json:"subject"`
	Body        string `json:"body"`
	Status      string `json:"status"`
	Attempts    int    `json:"attempts"`
	Error       string `json:"error,omitempty"`
}

func (s *Store) PrintNotifications(ctx context.Context, id int64) ([]PrintNotification, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,run_id,event_id,channel,subject,body,status,attempts,error FROM print_notifications WHERE run_id=$1 ORDER BY id DESC`, id)
	if err != nil {
		return nil, err
	}
	return collect(rows, func(row pgx.Rows) (PrintNotification, error) {
		var n PrintNotification
		err := row.Scan(&n.ID, &n.RunID, &n.EventID, &n.Channel, &n.Subject, &n.Body, &n.Status, &n.Attempts, &n.Error)
		return n, err
	})
}

// A transaction lease serializes workers. Transport retries use the same key.
// After 23h an ambiguous failure needs human reconciliation because Loops' key
// expires at 24h; we never blindly resend outside its deduplication window.
func (s *Store) PreparePrintNotificationLinks(ctx context.Context, baseURL string) error {
	_, err := s.pool.Exec(ctx, `UPDATE print_notifications SET body=body||E'\n\nView print request: '||$1||'/print-requests/'||run_id::text WHERE status='queued' AND first_attempt_at IS NULL AND position('View print request: ' in body)=0`, strings.TrimRight(baseURL, "/"))
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
		err = tx.QueryRow(ctx, `SELECT id,run_id,event_id,channel,destination,subject,body,status,attempts,first_attempt_at FROM print_notifications WHERE channel=$1 AND status IN ('queued','failed','sending') AND next_attempt_at<=now() ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 1`, channel).Scan(&n.ID, &n.RunID, &n.EventID, &n.Channel, &n.Destination, &n.Subject, &n.Body, &n.Status, &n.Attempts, &first)
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
		_, err = tx.Exec(ctx, `UPDATE print_notifications SET status='sending',first_attempt_at=coalesce(first_attempt_at,now()),attempts=$2,next_attempt_at=now()+interval '5 minutes' WHERE id=$1`, n.ID, n.Attempts)
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
		if err == nil && channel == "email" && tag.RowsAffected() == 1 {
			_, err = tx.Exec(ctx, `INSERT INTO sent_emails(kind,user_id,to_email,subject,preview,body,idempotency_key,status,error,release,print_run_id,print_event_id) SELECT 'print_'||e.stage,r.user_id,$2,$3,$3,$4,$5,$6,$7,'',r.id,$8 FROM print_runs r JOIN print_run_events e ON e.id=$8 WHERE r.id=$1`, n.RunID, n.Destination, n.Subject, n.Body, fmt.Sprintf("print-event-%d-email", n.EventID), status, errText, n.EventID)
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
