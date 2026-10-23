package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// PrintRequest is staff asking for postcards or posters to be printed and
// stocked in the warehouse (swag.PrintProducts).
type PrintRequest struct {
	ID            int64      `json:"id"`
	UserID        int64      `json:"user_id"`
	UserEmail     string     `json:"user_email,omitempty"`
	UserName      string     `json:"user_name,omitempty"`
	Product       string     `json:"product"`
	Title         string     `json:"title"`
	Quantity      int        `json:"quantity"`
	FrontURL      string     `json:"front_url"`
	BackURL       string     `json:"back_url"`
	Note          string     `json:"note"`
	NeededBy      *time.Time `json:"needed_by"`
	EstimateCents int        `json:"estimate_cents"`
	Status        string     `json:"status"` // submitted → ordered → stocked, or rejected
	AdminNote     string     `json:"admin_note"`
	SKU           string     `json:"sku"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

const printRequestSelect = `
	SELECT p.id, p.user_id, u.email, u.name, p.product, p.title, p.quantity, p.front_url, p.back_url, p.note,
		p.needed_by, p.estimate_cents, p.status, p.admin_note, p.sku, p.created_at, p.updated_at
	FROM print_requests p JOIN users u ON u.id = p.user_id`

func scanPrintRequest(row pgx.Row) (*PrintRequest, error) {
	var p PrintRequest
	err := row.Scan(&p.ID, &p.UserID, &p.UserEmail, &p.UserName, &p.Product, &p.Title, &p.Quantity, &p.FrontURL, &p.BackURL, &p.Note,
		&p.NeededBy, &p.EstimateCents, &p.Status, &p.AdminNote, &p.SKU, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &p, mapErr(err)
}

func (s *Store) CreatePrintRequest(ctx context.Context, p PrintRequest) (*PrintRequest, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO print_requests (user_id, product, title, quantity, front_url, back_url, note, needed_by, estimate_cents)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		p.UserID, p.Product, p.Title, p.Quantity, p.FrontURL, p.BackURL, p.Note, p.NeededBy, p.EstimateCents).Scan(&id)
	if err != nil {
		return nil, mapErr(err)
	}
	return s.GetPrintRequest(ctx, id)
}

func (s *Store) GetPrintRequest(ctx context.Context, id int64) (*PrintRequest, error) {
	return scanPrintRequest(s.pool.QueryRow(ctx, printRequestSelect+` WHERE p.id = $1`, id))
}

func (s *Store) ListPrintRequestsByUser(ctx context.Context, userID int64) ([]*PrintRequest, error) {
	rows, err := s.pool.Query(ctx, printRequestSelect+` WHERE p.user_id = $1 ORDER BY p.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	return collect(rows, func(r pgx.Rows) (*PrintRequest, error) { return scanPrintRequest(r) })
}

func (s *Store) ListPrintRequests(ctx context.Context) ([]*PrintRequest, error) {
	rows, err := s.pool.Query(ctx, printRequestSelect+` ORDER BY p.created_at DESC LIMIT 500`)
	if err != nil {
		return nil, err
	}
	return collect(rows, func(r pgx.Rows) (*PrintRequest, error) { return scanPrintRequest(r) })
}

// UpdatePrintRequest is an admin moving a print request along, with a note the
// requester sees and, once stocked, the warehouse SKU it's under.
func (s *Store) UpdatePrintRequest(ctx context.Context, id int64, status, adminNote, sku string) (*PrintRequest, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE print_requests SET status = $2, admin_note = $3, sku = $4, updated_at = now() WHERE id = $1`,
		id, status, adminNote, sku)
	if err != nil {
		return nil, mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return s.GetPrintRequest(ctx, id)
}
