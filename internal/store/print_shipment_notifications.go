package store

import (
	"context"
	"github.com/hackclub/merch/internal/swag"
)

func printStageLabel(stage swag.PrintStage) string {
	switch stage {
	case swag.PrintSubmitted:
		return "Submitted"
	case swag.PrintApproved:
		return "Approved"
	case swag.PrintOrdered:
		return "Ordered from printer"
	case swag.PrintInTransit:
		return "In transit"
	case swag.PrintReceived:
		return "Received at warehouse"
	case swag.PrintAvailable:
		return "Available"
	case swag.PrintRejected:
		return "Rejected"
	case swag.PrintCancelled:
		return "Cancelled"
	}
	return "Print request update"
}

// The job is inserted with the release transaction. Rendering uses the existing
// parcel email, then saves its exact bytes and key before any transport attempt.
func (s *Store) PreparePrintShipmentEmails(ctx context.Context, render func(*Request) (subject, preview, body, key string)) error {
	rows, err := s.pool.Query(ctx, `select id,request_id from print_notifications where channel='shipment_email' and body='' and first_attempt_at is null order by id limit 50`)
	if err != nil {
		return err
	}
	type job struct{ id, requestID int64 }
	jobs := []job{}
	for rows.Next() {
		var j job
		if err = rows.Scan(&j.id, &j.requestID); err != nil {
			rows.Close()
			return err
		}
		jobs = append(jobs, j)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, j := range jobs {
		request, err := s.GetRequest(ctx, j.requestID)
		if err != nil {
			return err
		}
		subject, preview, body, key := render(request)
		_, err = s.pool.Exec(ctx, `update print_notifications set subject=$2,body=$3,idempotency_key=$4,preview=$5 where id=$1 and body='' and first_attempt_at is null`, j.id, subject, body, key, preview)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) SkipUnconfiguredPrintSlack(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `update print_notifications set status='skipped',error='Slack DMs are not configured' where channel='slack' and status in ('queued','failed')`)
	return err
}
