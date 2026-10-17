package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// ---- Sent emails (every email to a requester, word for word) ----

// SentEmail is one email the app sent or tried to send (see sent_emails).
type SentEmail struct {
	ID             int64     `json:"id"`
	At             time.Time `json:"at"`
	Kind           string    `json:"kind"` // received, dispatched or rejected
	RequestID      *int64    `json:"request_id"`
	UserID         *int64    `json:"user_id"`
	To             string    `json:"to"`
	Subject        string    `json:"subject"`
	Preview        string    `json:"preview"`
	Body           string    `json:"body"` // plain text, as written
	IdempotencyKey string    `json:"idempotency_key"`
	Status         string    `json:"status"` // sent, failed, or unsent (no mailer configured)
	Error          string    `json:"error"`
	Release        string    `json:"release"` // the server build that wrote it
}

// LogEmail saves one email and returns it with its id and time.
func (s *Store) LogEmail(ctx context.Context, e SentEmail) (SentEmail, error) {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO sent_emails (kind, request_id, user_id, to_email, subject, preview, body, idempotency_key, status, error, release)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING id, at`,
		e.Kind, e.RequestID, e.UserID, e.To, e.Subject, e.Preview, e.Body, e.IdempotencyKey, e.Status, e.Error, e.Release).Scan(&e.ID, &e.At)
	return e, err
}

// EmailFilter narrows ListEmails; zero values match everything.
type EmailFilter struct {
	RequestID int64
	Kind      string
	Status    string
	Before    int64 // only emails with a smaller id (paging)
	Limit     int
}

// ListEmails lists logged emails, newest first.
func (s *Store) ListEmails(ctx context.Context, f EmailFilter) ([]SentEmail, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, at, kind, request_id, user_id, to_email, subject, preview, body, idempotency_key, status, error, release
		FROM sent_emails
		WHERE ($1 = 0 OR request_id = $1) AND ($2 = '' OR kind = $2) AND ($3 = '' OR status = $3) AND ($4 = 0 OR id < $4)
		ORDER BY id DESC LIMIT $5`, f.RequestID, f.Kind, f.Status, f.Before, f.Limit)
	if err != nil {
		return nil, err
	}
	return collect(rows, func(r pgx.Rows) (SentEmail, error) {
		var e SentEmail
		err := r.Scan(&e.ID, &e.At, &e.Kind, &e.RequestID, &e.UserID, &e.To, &e.Subject, &e.Preview, &e.Body, &e.IdempotencyKey, &e.Status, &e.Error, &e.Release)
		return e, err
	})
}
