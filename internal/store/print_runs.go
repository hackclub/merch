package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hackclub/merch/internal/swag"
	"github.com/jackc/pgx/v5"
)

var ErrPrintConflict = errors.New("print run changed; reload before acting")
var ErrPrintAction = swag.ErrPrintAction

type PrintRunData = swag.PrintRunData

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
func (s *Store) PrintRunsToPoll(ctx context.Context, now time.Time) ([]*PrintRun, error) {
	rows, err := s.pool.Query(ctx, printRunSelect+` WHERE EXISTS(SELECT 1 FROM print_run_po_links l WHERE l.run_id=r.id) ORDER BY r.data->>'last_poll_at' NULLS FIRST,r.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := []*PrintRun{}
	for rows.Next() {
		r, err := scanPrintRun(rows)
		if err != nil {
			return nil, err
		}
		if swag.PrintRunNeedsPoll(r.actionState(), now) {
			runs = append(runs, r)
			if len(runs) == 50 {
				break
			}
		}
	}
	return runs, rows.Err()
}

func (r *PrintRun) HasPrinterOrder() bool {
	state := r.actionState()
	return state.HasPrinterOrder()
}
func (r *PrintRun) actionState() swag.PrintRunState {
	return swag.PrintRunState{PrintRunData: r.PrintRunData, UserID: r.UserID, Stage: r.Stage, Origin: r.Origin,
		Request: swag.PrintRunRequest{Product: r.Request.Product, Quantity: r.Request.Quantity, InitialQuantity: r.Request.InitialQuantity, Address: r.Request.Address},
		Links:   r.Links, InternalNote: r.InternalNote, ShipmentRequestID: r.ShipmentRequestID, ShipmentStatus: r.ShipmentStatus}
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

type PrintAction = swag.PrintAction

func (s *Store) ActPrintRun(ctx context.Context, id int64, a PrintAction, messages PrintMessages) (*PrintRun, error) {
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
	previousETA := r.ETA

	state := r.actionState()
	if a.Action == "release" {
		err = tx.QueryRow(ctx, `select exists(select 1 from items where id=$1 and sku=$2 and name<>'' and cardinality(images)>0)`, r.ItemID, r.SKU).Scan(&state.ListingReady)
	}
	if a.Action == "resolve_notification" {
		err = tx.QueryRow(ctx, `select exists(select 1 from print_notifications where id=$1 and run_id=$2 and status='uncertain')`, a.NotificationID, id).Scan(&state.NotificationUncertain)
	}
	if a.Action == "audit_legacy" && r.LegacyReview && a.Note != "" && r.Stage == swag.PrintReceived && r.ShipmentRequestID != nil {
		err = tx.QueryRow(ctx, `select exists(select 1 from swag_requests s join swag_request_items i on i.request_id=s.id where s.id=$1 and s.user_id=$2 and i.sku=$3 and i.quantity>0)`, *r.ShipmentRequestID, r.UserID, a.SKU).Scan(&state.LegacyShipmentMatches)
		if err == nil && state.LegacyShipmentMatches {
			err = tx.QueryRow(ctx, `select visible AND name<>'' AND cardinality(images)>0 from items where id=$1 and sku=$2`, a.ItemID, a.SKU).Scan(&state.LegacyListingVisible)
		}
	}
	if err != nil {
		return nil, err
	}
	decision, err := swag.ApplyPrintAction(state, a, swag.PrintActor{ID: a.ActorID, Admin: !a.Requester && !a.Auto, Automatic: a.Auto}, now)
	if err != nil {
		return nil, err
	}
	r.PrintRunData = decision.Run.PrintRunData
	r.Stage, r.Links, r.InternalNote = decision.Run.Stage, decision.Run.Links, decision.Run.InternalNote
	invalid := func(msg string) (*PrintRun, error) { return nil, fmt.Errorf("%w: %s", ErrPrintAction, msg) }
	switch a.Action {
	case "preferences":
		if !a.SlackOptIn {
			_, err = tx.Exec(ctx, `update print_notifications set status='cancelled' where run_id=$1 and channel='slack' and status in ('queued','failed')`, id)
		}
	case "retry_notifications":
		_, err = tx.Exec(ctx, `update print_notifications set next_attempt_at=now() where run_id=$1 and status='failed'`, id)
	case "resolve_notification":
		status := "sent"
		if a.DeliveryOutcome == "not_sent" {
			status = "queued"
		}
		rows, e := tx.Exec(ctx, `update print_notifications set status=$3,first_attempt_at=null,next_attempt_at=now(),error='' where id=$1 and run_id=$2 and status='uncertain'`, a.NotificationID, id, status)
		if e != nil {
			return nil, e
		}
		if rows.RowsAffected() != 1 {
			return invalid("only an uncertain notification can be reconciled")
		}
	case "link", "replace_links":
		if a.Action == "replace_links" {
			if _, err = tx.Exec(ctx, `delete from print_run_po_links where run_id=$1`, id); err != nil {
				return nil, err
			}
		}
		for _, p := range a.Evidence {
			if _, err = tx.Exec(ctx, `INSERT INTO print_run_po_links(run_id,po_id,line_id,sku,evidence,confirmation_url,revision) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, p.POID, p.LineID, p.SKU, p, a.ConfirmationURL, r.Revision); err != nil {
				return nil, mapErr(err)
			}
		}
		if r.Request.Product != "custom" {
			r.ItemID, err = preparePrintItem(ctx, tx, r.Request, r.SKU)
		}
	case "observe":
		if a.PollError != "" {
			break
		}
		if r.SourceStale {
			for _, p := range r.Links {
				if _, err = tx.Exec(ctx, `update print_run_po_links set evidence=$4 where run_id=$1 and po_id=$2 and line_id=$3`, id, p.POID, p.LineID, p); err != nil {
					return nil, err
				}
			}
			break
		}
		for _, p := range a.Evidence {
			if _, err = tx.Exec(ctx, `INSERT INTO print_po_observations(run_id,po_id,line_id,synced_at,polled_at,evidence) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, id, p.POID, p.LineID, p.SyncedAt, p.PolledAt, p); err != nil {
				return nil, err
			}
			if _, err = tx.Exec(ctx, `UPDATE print_run_po_links SET evidence=$4 WHERE run_id=$1 AND po_id=$2 AND line_id=$3 AND sku=$5`, id, p.POID, p.LineID, p, p.SKU); err != nil {
				return nil, err
			}
		}
	case "release":
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
			err = tx.QueryRow(ctx, `INSERT INTO swag_requests(user_id,status,purpose,first_name,last_name,line_1,line_2,city,state,postal_code,country,phone_number,shipping_fee_cents,note) VALUES($1,$2,'staff',$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING id`, r.UserID, a.ReleaseStatus, ad.FirstName, ad.LastName, ad.Line1, ad.Line2, ad.City, ad.State, ad.PostalCode, ad.Country, ad.Phone, a.ReleaseFee, messages.Render(printMessageFacts(r, a, swag.PrintNoticeStage, false), messages.BaseURL).ShipmentNote).Scan(&rid)
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
	}
	if err != nil {
		return nil, err
	}
	if decision.SaveConfirmation {
		_, err = tx.Exec(ctx, `insert into print_printer_confirmations(run_id,version,url,actor_id) values($1,$2,$3,$4)`, id, r.Version+1, a.ConfirmationURL, a.ActorID)
		if err != nil {
			return nil, err
		}
	}
	recordEvent := decision.RecordEvent
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
		details := map[string]any{"from_stage": prev, "stage_changed": prev != r.Stage, "eta": r.ETA, "eta_changed": previousETA != r.ETA, "approved_quantity": r.ApprovedQuantity, "specifications": r.Specifications, "art_revision": r.Revision, "sku": r.SKU, "item_id": r.ItemID, "update_source": a.ETASource, "po_links": r.Links}
		err = tx.QueryRow(ctx, `INSERT INTO print_run_events(run_id,stage,action,actor_id,event_at,note,source,version,details) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`, id, r.Stage, decision.EventAction, actor, now, a.Note, decision.EventSource, r.Version, details).Scan(&eid)
		if err != nil {
			return nil, err
		}
		if decision.Notify {
			if err = queuePrintNotice(ctx, tx, r, eid, printMessageFacts(r, a, decision.Notice, decision.AppendETA), messages); err != nil {
				return nil, err
			}
		}
		if decision.NotifyFirstShipment && r.ShipmentRequestID != nil {
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

// preparePrintItem runs only after link validation, in the link transaction.
// Reuse an existing listing without changing its content or visibility.
func preparePrintItem(ctx context.Context, tx pgx.Tx, request PrintRequest, sku string) (int64, error) {
	var id int64
	err := tx.QueryRow(ctx, `select id from items where sku=$1`, sku).Scan(&id)
	if err == nil || !errors.Is(err, pgx.ErrNoRows) {
		return id, err
	}
	var sizeSKU bool
	err = tx.QueryRow(ctx, `select exists(select 1 from items,jsonb_array_elements(variants) v where v->>'sku'=$1)`, sku).Scan(&sizeSKU)
	if err != nil {
		return 0, err
	}
	if sizeSKU {
		return 0, fmt.Errorf("%w: the SKU is a size of another item; link the PO line under its own SKU", ErrPrintAction)
	}
	product, ok := swag.PrintProductByKey(request.Product)
	if !ok {
		return 0, ErrPrintAction
	}
	it := product.Item(sku, request.Title, request.Finish, request.Landscape)
	images := []string{}
	for _, u := range []string{request.FrontPreviewURL, request.BackPreviewURL} {
		if u != "" {
			images = append(images, u)
		}
	}
	if len(images) == 0 {
		images = []string{request.FrontURL}
	}
	err = tx.QueryRow(ctx, `insert into items(sku,name,images,visible,max_per_request,width_in,height_in,finish,thickness_in,packs)
 values($1,$2,$3,false,$4,$5,$6,$7,$8,$9) on conflict(sku) do update set sku=excluded.sku returning id`,
		sku, it.Name, images, it.MaxPerRequest, it.WidthIn, it.HeightIn, it.Finish, it.ThicknessIn, it.Packs).Scan(&id)
	return id, mapErr(err)
}

// createPrintRun is shared by the staff and author entry paths. The caller
// commits the source row, artwork, event and received messages together.
func createPrintRun(ctx context.Context, tx pgx.Tx, printID, authorID *int64, origin string, request PrintRequest, messages PrintMessages) (int64, error) {
	request.UserEmail, request.UserName = "", ""
	r := &PrintRun{UserID: request.UserID, Origin: origin, Stage: swag.PrintSubmitted, Version: 1, Request: request,
		PrintRunData: PrintRunData{SlackOptIn: request.SlackOptIn}}
	err := tx.QueryRow(ctx, `insert into print_runs(user_id,source_print_id,source_author_id,origin,stage,request,data,created_at) values($1,$2,$3,$4,'submitted',$5,$6,$7) returning id`,
		r.UserID, printID, authorID, origin, request, r.PrintRunData, request.CreatedAt).Scan(&r.ID)
	if err != nil {
		return 0, err
	}
	if _, err = tx.Exec(ctx, `insert into print_art_revisions(run_id,revision,front_url,back_url,created_at) values($1,1,$2,$3,$4)`, r.ID, request.FrontURL, request.BackURL, request.CreatedAt); err != nil {
		return 0, err
	}
	var eventID int64
	err = tx.QueryRow(ctx, `insert into print_run_events(run_id,stage,action,actor_id,event_at,source,version) values($1,'submitted','submit',$2,$3,$4,1) returning id`, r.ID, r.UserID, request.CreatedAt, origin).Scan(&eventID)
	if err != nil {
		return 0, err
	}
	if err = tx.QueryRow(ctx, `select email from users where id=$1`, r.UserID).Scan(&r.UserEmail); err != nil {
		return 0, err
	}
	return r.ID, queuePrintNotice(ctx, tx, r, eventID, printMessageFacts(r, PrintAction{}, swag.PrintNoticeStage, false), messages)
}

// ReorderPrintRun uses the parent's submitted version as a retry key. A fresh
// intentional reorder reloads the new parent version, giving a distinct run.
func (s *Store) ReorderPrintRun(ctx context.Context, id, version int64, messages PrintMessages) (*PrintRun, error) {
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
	if err = swag.CheckPrintReorder(p.Stage); err != nil {
		return nil, err
	}
	r := p.Request
	r.UserID, r.SlackOptIn = p.UserID, p.SlackOptIn
	created, err := insertPrintRequest(ctx, tx, r, messages)
	if err != nil {
		return nil, err
	}
	source := created.ID
	var idNew int64
	origin := p.Origin
	if origin == "legacy" {
		origin = "staff"
		if p.SourceAuthorID != nil {
			origin = "author"
		}
	}
	err = tx.QueryRow(ctx, `update print_runs set data=data||jsonb_build_object('parent_id',$2::bigint,'parent_version',$3::bigint,'sku',$5::text,'item_id',$6::bigint),origin=$4 where source_print_id=$1 returning id`, source, id, version, origin, p.SKU, p.ItemID).Scan(&idNew)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `update print_runs set version=version+1 where id=$1`, id)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `insert into print_run_events(run_id,stage,action,event_at,note,source,version) values($1,$2,'reorder',now(),$3,'requester',$4)`, id, p.Stage, messages.Render(PrintMessageFacts{NewRunID: idNew}, messages.BaseURL).ReorderNote, version+1)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.PrintRun(ctx, idNew)
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

	Stage swag.PrintStage `json:"-"`
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
func (s *Store) DeliverPrintNotifications(ctx context.Context, channel string, send func(PrintNotification) error) error {
	for i := 0; i < 20; i++ {
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return err
		}
		var n PrintNotification
		var first *time.Time
		err = tx.QueryRow(ctx, `SELECT n.id,n.run_id,n.event_id,n.channel,n.destination,n.subject,n.body,n.status,n.attempts,n.first_attempt_at,n.idempotency_key,n.request_id,n.preview,e.stage FROM print_notifications n JOIN print_run_events e ON e.id=n.event_id WHERE n.channel=$1 AND n.body<>'' AND n.status IN ('queued','failed','sending') AND n.next_attempt_at<=now() ORDER BY n.id FOR UPDATE OF n SKIP LOCKED LIMIT 1`, channel).Scan(&n.ID, &n.RunID, &n.EventID, &n.Channel, &n.Destination, &n.Subject, &n.Body, &n.Status, &n.Attempts, &first, &n.IdempotencyKey, &n.RequestID, &n.Preview, &n.Stage)
		if errors.Is(err, pgx.ErrNoRows) {
			tx.Rollback(ctx)
			return nil
		}
		if err != nil {
			tx.Rollback(ctx)
			return err
		}
		if first != nil && time.Since(*first) > 23*time.Hour {
			_, err = tx.Exec(ctx, `UPDATE print_notifications SET status='uncertain',error=$2 WHERE id=$1`, n.ID, "Transport deduplication window expired; reconcile delivery before retrying.")
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
			kind := "print_" + string(n.Stage)
			if channel == "shipment_email" {
				kind = "received"
			}
			preview := n.Preview
			if preview == "" {
				preview = n.Subject
			}
			_, err = tx.Exec(ctx, `INSERT INTO sent_emails(kind,user_id,to_email,subject,preview,body,idempotency_key,status,error,release,print_run_id,print_event_id,request_id) SELECT $9,r.user_id,$2,$3,$11,$4,$5,$6,$7,'',r.id,$8,$10 FROM print_runs r WHERE r.id=$1`, n.RunID, n.Destination, n.Subject, n.Body, n.IdempotencyKey, status, errText, n.EventID, kind, n.RequestID, preview)
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
