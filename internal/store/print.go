package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hackclub/merch/internal/swag"
)

// PrintRequest is staff asking for postcards or posters to be printed and
// stocked in the warehouse (swag.PrintProducts).
type PrintRequest struct {
	ID            int64      `json:"id"`
	UserID        int64      `json:"user_id"`
	UserEmail     string     `json:"user_email,omitempty"`
	UserName      string     `json:"user_name,omitempty"`
	Product       string     `json:"product"`
	Title         string     `json:"title"`    // what it's called in the warehouse and the shop
	Finish        string     `json:"finish"`   // the paper; '' on older requests (the product's default)
	Program       string     `json:"program"`  // the program it's for
	Deadline      string     `json:"deadline"` // the program's final deadline, in their words
	Quantity      int        `json:"quantity"`
	FrontURL      string     `json:"front_url"`
	BackURL       string     `json:"back_url"`
	Note          string     `json:"note"`
	NeededBy      *time.Time `json:"needed_by"` // older requests only
	EstimateCents int        `json:"estimate_cents"`
	Status        string     `json:"status"` // submitted → ordered → stocked, or rejected
	AdminNote     string     `json:"admin_note"`
	SKU           string     `json:"sku"`
	// The first shipment to them once it's stocked: how many, where, and the
	// swag request it became.
	InitialQuantity   int           `json:"initial_quantity"`
	Address           *swag.Address `json:"address"`
	ShipmentRequestID *int64        `json:"shipment_request_id"`
	// The art trimmed and shrunk to a web JPEG, for the item it's listed as.
	FrontPreviewURL string    `json:"front_preview_url"`
	BackPreviewURL  string    `json:"back_preview_url"`
	Landscape       bool      `json:"landscape"` // the art is wider than tall
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`

	// Its canonical print run (print_runs), and whether they want Slack DMs about it.
	PrintRunID *int64          `json:"print_run_id,omitempty"`
	PrintStage swag.PrintStage `json:"print_stage,omitempty"`
	SlackOptIn bool            `json:"slack_opt_in"`
}

const printRequestSelect = `
	SELECT p.id, p.user_id, u.email, u.name, p.product, p.title, p.quantity, p.front_url, p.back_url, p.note,
		p.needed_by, p.estimate_cents, p.status, p.admin_note, p.sku, p.created_at, p.updated_at,
		p.initial_quantity, p.address, p.shipment_request_id, p.front_preview_url, p.back_preview_url, p.landscape,
		p.program, p.deadline, p.finish, p.slack_opt_in
	FROM print_requests p JOIN users u ON u.id = p.user_id`

func scanPrintRequest(row pgx.Row) (*PrintRequest, error) {
	var p PrintRequest
	err := row.Scan(&p.ID, &p.UserID, &p.UserEmail, &p.UserName, &p.Product, &p.Title, &p.Quantity, &p.FrontURL, &p.BackURL, &p.Note,
		&p.NeededBy, &p.EstimateCents, &p.Status, &p.AdminNote, &p.SKU, &p.CreatedAt, &p.UpdatedAt,
		&p.InitialQuantity, &p.Address, &p.ShipmentRequestID, &p.FrontPreviewURL, &p.BackPreviewURL, &p.Landscape,
		&p.Program, &p.Deadline, &p.Finish, &p.SlackOptIn)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &p, mapErr(err)
}

func (s *Store) CreatePrintRequest(ctx context.Context, p PrintRequest) (*PrintRequest, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO print_requests (user_id, product, title, quantity, front_url, back_url, note, needed_by, estimate_cents,
			initial_quantity, address, front_preview_url, back_preview_url, landscape, program, deadline, finish, slack_opt_in)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18) RETURNING id`,
		p.UserID, p.Product, p.Title, p.Quantity, p.FrontURL, p.BackURL, p.Note, p.NeededBy, p.EstimateCents,
		p.InitialQuantity, p.Address, p.FrontPreviewURL, p.BackPreviewURL, p.Landscape, p.Program, p.Deadline, p.Finish, p.SlackOptIn).Scan(&id)
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
