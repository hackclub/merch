package store

import (
	"context"
	"crypto/md5"
	"fmt"
	"strings"

	"github.com/hackclub/merch/internal/swag"
	"github.com/jackc/pgx/v5"
)

// PrintMessageFacts contains public facts, not private HQ notes or PO details.
type PrintMessageFacts struct {
	RunID             int64
	NewRunID          int64
	Stage             swag.PrintStage
	Notice            swag.PrintNoticeKind
	Title             string
	RequestedQuantity int
	ApprovedQuantity  int
	FirstQuantity     int
	ETA               string
	UpdateETA         string
	ETASource         string
	Note              string
	AppendETA         bool
	PrinterCancelled  bool
	ShipmentRequestID *int64
	ShipmentStatus    swag.Status
	ReleaseStatus     swag.Status
}
type PrintMessage struct{ Subject, Body, SlackBody, ShipmentNote, ReorderNote string }
type PrintMessages struct {
	BaseURL string
	Render  func(PrintMessageFacts, string) PrintMessage
}

func printMessageFacts(r *PrintRun, a PrintAction, notice swag.PrintNoticeKind, appendETA bool) PrintMessageFacts {
	return PrintMessageFacts{RunID: r.ID, Stage: r.Stage, Notice: notice, Title: r.Request.Title, RequestedQuantity: r.Request.Quantity,
		ApprovedQuantity: r.ApprovedQuantity, FirstQuantity: r.Request.InitialQuantity, ETA: r.ETA, UpdateETA: a.ETA, ETASource: a.ETASource, Note: a.Note,
		AppendETA: appendETA, PrinterCancelled: a.ConfirmationURL != "", ShipmentRequestID: r.ShipmentRequestID, ShipmentStatus: r.ShipmentStatus, ReleaseStatus: a.ReleaseStatus}
}

// Keep the original key format so retries remain deduplicated across releases.
func printNotificationKey(baseURL string, eventID int64, channel string) string {
	return fmt.Sprintf("merch-print-%x", md5.Sum([]byte(fmt.Sprintf("%s:%d:%s", strings.TrimRight(baseURL, "/"), eventID, channel))))
}

func queuePrintNotice(ctx context.Context, tx pgx.Tx, r *PrintRun, eventID int64, facts PrintMessageFacts, messages PrintMessages) error {
	if messages.Render == nil {
		return fmt.Errorf("print message renderer required")
	}
	message := messages.Render(facts, messages.BaseURL)
	if message.Body == "" {
		return nil
	}
	_, err := tx.Exec(ctx, `insert into print_notifications(run_id,event_id,channel,destination,subject,body,idempotency_key) values($1,$2,'email',$3,$4,$5,$6)`, r.ID, eventID, r.UserEmail, message.Subject, message.Body, printNotificationKey(messages.BaseURL, eventID, "email"))
	if err != nil {
		return err
	}
	if r.SlackOptIn {
		_, err = tx.Exec(ctx, `insert into print_notifications(run_id,event_id,channel,destination,subject,body,idempotency_key) select $1,$2,'slack',slack_id,$3,$4,$5 from users where id=$6 and slack_id<>''`, r.ID, eventID, message.Subject, message.SlackBody, printNotificationKey(messages.BaseURL, eventID, "slack"), r.UserID)
	}
	return err
}
