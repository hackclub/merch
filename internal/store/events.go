package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

// Event is one thing someone did on the site (see usage_events).
type Event struct {
	ID      int64           `json:"id"`
	At      time.Time       `json:"at"` // zero: now
	Source  string          `json:"source"`
	Name    string          `json:"name"`
	Visitor string          `json:"visitor"`
	Visit   string          `json:"visit"`
	UserID  *int64          `json:"user_id"`
	Path    string          `json:"path"`
	Props   json.RawMessage `json:"props"`   // a JSON object; nil: {}
	Release string          `json:"release"` // the site build it came from ("" unknown)
}

// RecordEvents saves usage events in one round trip.
func (s *Store) RecordEvents(ctx context.Context, evs []Event) error {
	b := &pgx.Batch{}
	for _, e := range evs {
		var at *time.Time
		if !e.At.IsZero() {
			at = &e.At
		}
		props := e.Props
		if len(props) == 0 {
			props = json.RawMessage(`{}`)
		}
		b.Queue(`INSERT INTO usage_events (at, source, name, visitor, visit, user_id, path, props, release)
			VALUES (coalesce($1, now()), $2, $3, $4, $5, $6, $7, $8, $9)`,
			at, e.Source, e.Name, e.Visitor, e.Visit, e.UserID, e.Path, string(props), e.Release)
	}
	return s.pool.SendBatch(ctx, b).Close()
}

// RecentEvents lists the newest events, all of them or those named name.
func (s *Store) RecentEvents(ctx context.Context, name string, limit int) ([]Event, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, at, source, name, visitor, visit, user_id, path, props::text, release
		FROM usage_events WHERE $1 = '' OR name = $1 ORDER BY at DESC, id DESC LIMIT $2`, name, limit)
	if err != nil {
		return nil, err
	}
	return collect(rows, func(r pgx.Rows) (Event, error) {
		var e Event
		var props string
		err := r.Scan(&e.ID, &e.At, &e.Source, &e.Name, &e.Visitor, &e.Visit, &e.UserID, &e.Path, &props, &e.Release)
		e.Props = json.RawMessage(props)
		return e, err
	})
}
