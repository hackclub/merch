// Package store is the Postgres persistence layer. Business rules live in
// package swag; the store only guarantees atomicity (e.g. a request's limit
// check and insert happen under one per-user lock).
package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hackclub/merch/internal/auth"
	"github.com/hackclub/merch/internal/swag"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrConflict      = errors.New("already exists")
	ErrBadTransition = errors.New("request is not in a state that allows this")
)

type Store struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// ---- users & sessions ----

type User struct {
	ID                 int64         `json:"id"`
	HCAID              string        `json:"-"`
	Email              string        `json:"email"`
	Name               string        `json:"name"`
	SlackID            string        `json:"slack_id"`
	VerificationStatus string        `json:"verification_status"`
	Address            *swag.Address `json:"address,omitempty"` // from Hack Club Auth, for prefilling
}

const userCols = `id, coalesce(hca_id, ''), email, name, slack_id, verification_status, address`

func scanUser(row pgx.Row) (*User, error) {
	var u User
	err := row.Scan(&u.ID, &u.HCAID, &u.Email, &u.Name, &u.SlackID, &u.VerificationStatus, &u.Address)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &u, err
}

// UpsertUser records a Hack Club Auth sign-in. A returning user is matched by
// HCA id; a first sign-in claims any imported placeholder with the same email
// (so historical requests count toward their limits); otherwise a user is created.
func (s *Store) UpsertUser(ctx context.Context, id auth.Identity) (*User, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	args := []any{id.ID, id.Email, id.Name, id.SlackID, id.VerificationStatus, id.Address}
	u, err := scanUser(tx.QueryRow(ctx, `
		UPDATE users SET email = $2, name = $3, slack_id = $4, verification_status = $5,
			address = coalesce($6, address), last_login_at = now()
		WHERE hca_id = $1 RETURNING `+userCols, args...))
	if errors.Is(err, ErrNotFound) {
		u, err = scanUser(tx.QueryRow(ctx, `
			UPDATE users SET hca_id = $1, email = $2, name = $3, slack_id = $4, verification_status = $5,
				address = coalesce($6, address), last_login_at = now()
			WHERE id = (SELECT id FROM users WHERE hca_id IS NULL AND lower(email) = lower($2) LIMIT 1)
			RETURNING `+userCols, args...))
	}
	if errors.Is(err, ErrNotFound) {
		u, err = scanUser(tx.QueryRow(ctx, `
			INSERT INTO users (hca_id, email, name, slack_id, verification_status, address)
			VALUES ($1, $2, $3, $4, $5, $6) RETURNING `+userCols, args...))
	}
	if err != nil {
		return nil, err
	}
	return u, tx.Commit(ctx)
}

func hashToken(tok string) []byte {
	h := sha256.Sum256([]byte(tok))
	return h[:]
}

// CreateSession returns an opaque bearer token; only its hash is stored.
func (s *Store) CreateSession(ctx context.Context, userID int64, ttl time.Duration) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	tok := base64.RawURLEncoding.EncodeToString(b)
	_, err := s.pool.Exec(ctx, `INSERT INTO sessions (token_hash, user_id, expires_at) VALUES ($1, $2, $3)`,
		hashToken(tok), userID, time.Now().Add(ttl))
	return tok, err
}

func (s *Store) UserBySession(ctx context.Context, tok string) (*User, error) {
	return scanUser(s.pool.QueryRow(ctx, `
		SELECT u.id, coalesce(u.hca_id, ''), u.email, u.name, u.slack_id, u.verification_status, u.address
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.expires_at > now()`, hashToken(tok)))
}

func (s *Store) DeleteSession(ctx context.Context, tok string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, hashToken(tok))
	return err
}

// ---- settings ----

func (s *Store) GetSettings(ctx context.Context) (swag.Settings, error) {
	var st swag.Settings
	err := s.pool.QueryRow(ctx, `SELECT requests_open, max_items_per_request, request_cooldown_days, international_shipping_fee_cents FROM settings`).
		Scan(&st.RequestsOpen, &st.MaxItemsPerRequest, &st.RequestCooldownDays, &st.InternationalShippingFeeCents)
	return st, err
}

func (s *Store) UpdateSettings(ctx context.Context, st swag.Settings) error {
	_, err := s.pool.Exec(ctx, `UPDATE settings SET requests_open = $1, max_items_per_request = $2,
		request_cooldown_days = $3, international_shipping_fee_cents = $4, updated_at = now()`,
		st.RequestsOpen, st.MaxItemsPerRequest, st.RequestCooldownDays, st.InternationalShippingFeeCents)
	return err
}

// ---- items ----

const itemCols = `id, sku, name, description, images, visible, sort_order, max_per_request, max_per_user, width_in, height_in, variants,
	finish, thickness_in, photos, packs, binding, pages, ships_with, club_only, clip`

func scanItem(row pgx.Row) (swag.Item, error) {
	var it swag.Item
	err := row.Scan(&it.ID, &it.SKU, &it.Name, &it.Description, &it.Images, &it.Visible, &it.SortOrder, &it.MaxPerRequest, &it.MaxPerUser, &it.WidthIn, &it.HeightIn, &it.Variants,
		&it.Finish, &it.ThicknessIn, &it.Photos, &it.Packs, &it.Binding, &it.Pages, &it.ShipsWith, &it.ClubOnly, &it.Clip)
	if errors.Is(err, pgx.ErrNoRows) {
		return it, ErrNotFound
	}
	if it.Images == nil {
		it.Images = []string{}
	}
	if it.Variants == nil {
		it.Variants = []swag.Variant{}
	}
	if it.Photos == nil {
		it.Photos = []swag.Photo{}
	}
	if it.Pages == nil {
		it.Pages = []string{}
	}
	if it.ShipsWith == nil {
		it.ShipsWith = []string{}
	}
	return it, mapErr(err)
}

func (s *Store) ListItems(ctx context.Context, visibleOnly bool) ([]swag.Item, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+itemCols+` FROM items WHERE visible OR NOT $1 ORDER BY sort_order, name`, visibleOnly)
	if err != nil {
		return nil, err
	}
	return collect(rows, func(r pgx.Rows) (swag.Item, error) { return scanItem(r) })
}

func (s *Store) GetItem(ctx context.Context, id int64) (swag.Item, error) {
	return scanItem(s.pool.QueryRow(ctx, `SELECT `+itemCols+` FROM items WHERE id = $1`, id))
}

func (s *Store) CreateItem(ctx context.Context, it swag.Item) (swag.Item, error) {
	return s.saveItem(ctx, it, false, `
		INSERT INTO items (sku, name, description, images, visible, sort_order, max_per_request, max_per_user, width_in, height_in, variants,
			finish, thickness_in, photos, packs, binding, pages, ships_with, club_only, clip)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20) RETURNING `+itemCols)
}

func (s *Store) UpdateItem(ctx context.Context, it swag.Item) (swag.Item, error) {
	return s.saveItem(ctx, it, true, `
		UPDATE items SET sku = $1, name = $2, description = $3, images = $4, visible = $5,
			sort_order = $6, max_per_request = $7, max_per_user = $8, width_in = $9, height_in = $10, variants = $11,
			finish = $12, thickness_in = $13, photos = $14, packs = $15, binding = $16, pages = $17, ships_with = $18, club_only = $19, clip = $20, updated_at = now()
		WHERE id = $21 RETURNING `+itemCols)
}

// saveItem writes an item and keeps every warehouse SKU on one item: a SKU
// already sold as another item's size is a conflict, and an item listed under
// one of this item's size SKUs (e.g. imported per-size shirts) is folded into
// this item, request history included.
func (s *Store) saveItem(ctx context.Context, it swag.Item, update bool, q string) (swag.Item, error) {
	if it.Variants == nil {
		it.Variants = []swag.Variant{}
	}
	skus := []string{it.SKU}
	for _, v := range it.Variants {
		skus = append(skus, v.SKU)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return it, err
	}
	defer tx.Rollback(ctx)
	var taken string
	err = tx.QueryRow(ctx, `
		SELECT v->>'sku' FROM items o, jsonb_array_elements(o.variants) v
		WHERE o.id <> $1 AND v->>'sku' = ANY($2) LIMIT 1`, it.ID, skus).Scan(&taken)
	if err == nil {
		return it, fmt.Errorf("%w: %s is a size of another item", ErrConflict, taken)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return it, err
	}
	args := []any{it.SKU, it.Name, it.Description, nonNil(it.Images), it.Visible, it.SortOrder, it.MaxPerRequest, it.MaxPerUser, it.WidthIn, it.HeightIn, it.Variants,
		it.Finish, it.ThicknessIn, photos(it.Photos), it.Packs, it.Binding, nonNil(it.Pages), nonNil(it.ShipsWith), it.ClubOnly, it.Clip}
	if update {
		args = append(args, it.ID)
	}
	saved, err := scanItem(tx.QueryRow(ctx, q, args...))
	if err != nil {
		return saved, err
	}
	for _, v := range saved.Variants {
		if _, err := tx.Exec(ctx, `
			UPDATE swag_request_items SET item_id = $1, variant = $3
			WHERE sku = $2 AND item_id IN (SELECT id FROM items WHERE id = $1 OR sku = $2)`, saved.ID, v.SKU, v.Label); err != nil {
			return saved, mapErr(err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM items WHERE sku = $2 AND id <> $1`, saved.ID, v.SKU); err != nil {
			return saved, mapErr(err)
		}
	}
	return saved, tx.Commit(ctx)
}

// AddItemPhoto appends a real-world photo to an item.
func (s *Store) AddItemPhoto(ctx context.Context, id int64, p swag.Photo) (swag.Item, error) {
	return scanItem(s.pool.QueryRow(ctx, `
		UPDATE items SET photos = photos || jsonb_build_array($2::jsonb), updated_at = now()
		WHERE id = $1 RETURNING `+itemCols, id, p))
}

func photos(ps []swag.Photo) []swag.Photo {
	if ps == nil {
		return []swag.Photo{}
	}
	return ps
}

// AddItemImage appends an image URL to the end of an item's gallery.
func (s *Store) AddItemImage(ctx context.Context, id int64, url string) (swag.Item, error) {
	return scanItem(s.pool.QueryRow(ctx, `
		UPDATE items SET images = array_append(images, $2), updated_at = now()
		WHERE id = $1 RETURNING `+itemCols, id, url))
}

// AddItemPage appends a page image to the end of a booklet's pages.
func (s *Store) AddItemPage(ctx context.Context, id int64, url string) (swag.Item, error) {
	return scanItem(s.pool.QueryRow(ctx, `
		UPDATE items SET pages = array_append(pages, $2), updated_at = now()
		WHERE id = $1 RETURNING `+itemCols, id, url))
}

// ItemsBySKU returns the items listed under the given SKUs, hidden ones included.
func (s *Store) ItemsBySKU(ctx context.Context, skus []string) ([]swag.Item, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+itemCols+` FROM items WHERE sku = ANY($1) ORDER BY sort_order, name`, skus)
	if err != nil {
		return nil, err
	}
	return collect(rows, func(r pgx.Rows) (swag.Item, error) { return scanItem(r) })
}

// nonNil keeps NOT NULL array columns from receiving SQL NULL.
func nonNil(ss []string) []string {
	if ss == nil {
		return []string{}
	}
	return ss
}

// ---- swag requests ----

type RequestLine struct {
	ItemID   int64  `json:"item_id"`
	SKU      string `json:"sku"` // the warehouse SKU shipped (the size's SKU for sized items)
	Name     string `json:"name"`
	Variant  string `json:"variant,omitempty"` // size label, e.g. "M"
	Quantity int    `json:"quantity"`
}

type Request struct {
	ID               int64         `json:"id"`
	UserID           int64         `json:"user_id"`
	UserEmail        string        `json:"user_email,omitempty"`
	Status           swag.Status   `json:"status"`
	Purpose          swag.Purpose  `json:"purpose"`
	Address          swag.Address  `json:"address"`
	ShippingFeeCents int           `json:"shipping_fee_cents"`
	PaidAt           *time.Time    `json:"paid_at"`
	Note             string        `json:"note"`
	AdminNote        string        `json:"admin_note"`
	InternalNote     string        `json:"internal_note,omitempty"` // admin-only
	TheseusOrderID   string        `json:"theseus_order_id"`
	TrackingNumber   string        `json:"tracking_number"`
	Carrier          string        `json:"carrier"`
	MailedAt         *time.Time    `json:"mailed_at"`
	Source           string        `json:"source"`
	AirtableRecordID string        `json:"airtable_record_id,omitempty"`
	CreatedAt        time.Time     `json:"created_at"`
	Lines            []RequestLine `json:"lines"`
}

type NewRequest struct {
	UserID           int64
	Address          swag.Address
	Purpose          swag.Purpose
	Lines            []swag.Line
	Status           swag.Status
	ShippingFeeCents int
	Note             string
}

// UsageCheck receives the user's prior per-item quantities and most recent
// request time (both excluding rejected/cancelled requests) and may veto.
type UsageCheck func(prior map[int64]int, lastRequestAt *time.Time) error

// CreateRequest locks the user row, computes usage, runs check, and inserts —
// all in one transaction so concurrent requests can't both slip under a limit.
func (s *Store) CreateRequest(ctx context.Context, nr NewRequest, check UsageCheck) (*Request, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id = $1 FOR UPDATE`, nr.UserID); err != nil {
		return nil, err
	}
	prior := map[int64]int{}
	rows, err := tx.Query(ctx, `
		SELECT ri.item_id, sum(ri.quantity)::int FROM swag_request_items ri
		JOIN swag_requests r ON r.id = ri.request_id
		WHERE r.user_id = $1 AND r.status NOT IN ('rejected', 'cancelled')
		GROUP BY ri.item_id`, nr.UserID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var q int
		if err := rows.Scan(&id, &q); err != nil {
			return nil, err
		}
		prior[id] = q
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var last *time.Time
	if err := tx.QueryRow(ctx, `SELECT max(created_at) FROM swag_requests WHERE user_id = $1 AND status NOT IN ('rejected', 'cancelled')`, nr.UserID).Scan(&last); err != nil {
		return nil, err
	}
	if err := check(prior, last); err != nil {
		return nil, err
	}

	a := nr.Address
	var id int64
	err = tx.QueryRow(ctx, `
		INSERT INTO swag_requests (user_id, status, first_name, last_name, line_1, line_2, city, state, postal_code, country, phone_number, shipping_fee_cents, note, purpose)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14) RETURNING id`,
		nr.UserID, nr.Status, a.FirstName, a.LastName, a.Line1, a.Line2, a.City, a.State, a.PostalCode, a.Country, a.Phone, nr.ShippingFeeCents, nr.Note, nr.Purpose).Scan(&id)
	if err != nil {
		return nil, mapErr(err)
	}
	for _, l := range nr.Lines {
		tag, err := tx.Exec(ctx, `
			INSERT INTO swag_request_items (request_id, item_id, quantity, sku, variant)
			SELECT $1, i.id, $3, coalesce(nullif($4, ''), i.sku),
				coalesce((SELECT v->>'label' FROM jsonb_array_elements(i.variants) v WHERE v->>'sku' = $4 LIMIT 1), '')
			FROM items i WHERE i.id = $2`, id, l.ItemID, l.Quantity, l.SKU)
		if err != nil {
			return nil, mapErr(err)
		}
		if tag.RowsAffected() == 0 {
			return nil, fmt.Errorf("item %d: %w", l.ItemID, ErrNotFound)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.GetRequest(ctx, id)
}

const requestSelect = `
	SELECT r.id, r.user_id, u.email, r.status, r.purpose, r.first_name, r.last_name, r.line_1, r.line_2, r.city, r.state,
		r.postal_code, r.country, r.phone_number, r.shipping_fee_cents, r.paid_at, r.note, r.admin_note, r.internal_note,
		coalesce(r.theseus_order_id, ''), r.tracking_number, r.carrier, r.mailed_at, r.source, r.airtable_record_id, r.created_at
	FROM swag_requests r JOIN users u ON u.id = r.user_id`

func scanRequest(row pgx.Row) (*Request, error) {
	var r Request
	a := &r.Address
	err := row.Scan(&r.ID, &r.UserID, &r.UserEmail, &r.Status, &r.Purpose, &a.FirstName, &a.LastName, &a.Line1, &a.Line2, &a.City, &a.State,
		&a.PostalCode, &a.Country, &a.Phone, &r.ShippingFeeCents, &r.PaidAt, &r.Note, &r.AdminNote, &r.InternalNote,
		&r.TheseusOrderID, &r.TrackingNumber, &r.Carrier, &r.MailedAt, &r.Source, &r.AirtableRecordID, &r.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &r, err
}

func (s *Store) GetRequest(ctx context.Context, id int64) (*Request, error) {
	r, err := scanRequest(s.pool.QueryRow(ctx, requestSelect+` WHERE r.id = $1`, id))
	if err != nil {
		return nil, err
	}
	return r, s.attachLines(ctx, []*Request{r})
}

func (s *Store) ListRequestsByUser(ctx context.Context, userID int64) ([]*Request, error) {
	return s.listRequests(ctx, requestSelect+` WHERE r.user_id = $1 ORDER BY r.created_at DESC`, userID)
}

// ListRequests lists all requests, optionally filtered by status ("" = all).
func (s *Store) ListRequests(ctx context.Context, status swag.Status) ([]*Request, error) {
	return s.listRequests(ctx, requestSelect+` WHERE $1 = '' OR r.status = $1 ORDER BY r.created_at DESC LIMIT 500`, string(status))
}

func (s *Store) listRequests(ctx context.Context, q string, args ...any) ([]*Request, error) {
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	reqs, err := collect(rows, func(r pgx.Rows) (*Request, error) { return scanRequest(r) })
	if err != nil {
		return nil, err
	}
	return reqs, s.attachLines(ctx, reqs)
}

func (s *Store) attachLines(ctx context.Context, reqs []*Request) error {
	if len(reqs) == 0 {
		return nil
	}
	ids := make([]int64, len(reqs))
	byID := map[int64]*Request{}
	for i, r := range reqs {
		ids[i] = r.ID
		byID[r.ID] = r
		r.Lines = []RequestLine{}
	}
	rows, err := s.pool.Query(ctx, `
		SELECT ri.request_id, ri.item_id, ri.sku, i.name, ri.variant, ri.quantity
		FROM swag_request_items ri JOIN items i ON i.id = ri.item_id
		WHERE ri.request_id = ANY($1) ORDER BY i.sort_order, i.name, ri.sku`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var rid int64
		var l RequestLine
		if err := rows.Scan(&rid, &l.ItemID, &l.SKU, &l.Name, &l.Variant, &l.Quantity); err != nil {
			return err
		}
		byID[rid].Lines = append(byID[rid].Lines, l)
	}
	return rows.Err()
}

// TransitionRequest moves a request to status `to` if swag.CanTransition allows it.
func (s *Store) TransitionRequest(ctx context.Context, id int64, to swag.Status, adminNote string) (*Request, error) {
	return s.transition(ctx, id, to, `admin_note = CASE WHEN $3 = '' THEN admin_note ELSE $3 END`, adminNote)
}

// MarkPaid records the HCB shipping payment and moves the request to review.
func (s *Store) MarkPaid(ctx context.Context, id int64) (*Request, error) {
	return s.transition(ctx, id, swag.StatusPending, `paid_at = now(), admin_note = admin_note || $3`, "")
}

// MarkDispatched records the mail.hackclub.com order id.
func (s *Store) MarkDispatched(ctx context.Context, id int64, theseusOrderID string) (*Request, error) {
	return s.transition(ctx, id, swag.StatusDispatched, `theseus_order_id = $3`, theseusOrderID)
}

func (s *Store) transition(ctx context.Context, id int64, to swag.Status, set string, arg string) (*Request, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var from swag.Status
	err = tx.QueryRow(ctx, `SELECT status FROM swag_requests WHERE id = $1 FOR UPDATE`, id).Scan(&from)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	if !swag.CanTransition(from, to) {
		return nil, fmt.Errorf("%w (%s -> %s)", ErrBadTransition, from, to)
	}
	if _, err := tx.Exec(ctx, `UPDATE swag_requests SET status = $2, updated_at = now(), `+set+` WHERE id = $1`, id, to, arg); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.GetRequest(ctx, id)
}

func (s *Store) SetTracking(ctx context.Context, id int64, tracking, carrier string) (*Request, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE swag_requests SET tracking_number = $2, carrier = $3, updated_at = now() WHERE id = $1`, id, tracking, carrier)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return s.GetRequest(ctx, id)
}

// ---- author submissions ----

type Submission struct {
	ID          int64     `json:"id"`
	UserID      int64     `json:"user_id"`
	UserEmail   string    `json:"user_email,omitempty"`
	Program     string    `json:"program"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Kind        string    `json:"kind"`
	FileURL     string    `json:"file_url"`
	Quantity    int       `json:"quantity"`
	Status      string    `json:"status"`
	AdminNote   string    `json:"admin_note"`
	SKU         string    `json:"sku"`
	CreatedAt   time.Time `json:"created_at"`
}

const submissionSelect = `
	SELECT s.id, s.user_id, u.email, s.program, s.title, s.description, s.kind, s.file_url, s.quantity,
		s.status, s.admin_note, s.sku, s.created_at
	FROM author_submissions s JOIN users u ON u.id = s.user_id`

func scanSubmission(row pgx.Row) (*Submission, error) {
	var s Submission
	err := row.Scan(&s.ID, &s.UserID, &s.UserEmail, &s.Program, &s.Title, &s.Description, &s.Kind, &s.FileURL, &s.Quantity,
		&s.Status, &s.AdminNote, &s.SKU, &s.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &s, mapErr(err)
}

func (s *Store) CreateSubmission(ctx context.Context, sub Submission) (*Submission, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO author_submissions (user_id, program, title, description, kind, file_url, quantity)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		sub.UserID, sub.Program, sub.Title, sub.Description, sub.Kind, sub.FileURL, sub.Quantity).Scan(&id)
	if err != nil {
		return nil, mapErr(err)
	}
	return scanSubmission(s.pool.QueryRow(ctx, submissionSelect+` WHERE s.id = $1`, id))
}

func (s *Store) ListSubmissionsByUser(ctx context.Context, userID int64) ([]*Submission, error) {
	rows, err := s.pool.Query(ctx, submissionSelect+` WHERE s.user_id = $1 ORDER BY s.created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	return collect(rows, func(r pgx.Rows) (*Submission, error) { return scanSubmission(r) })
}

func (s *Store) ListSubmissions(ctx context.Context) ([]*Submission, error) {
	rows, err := s.pool.Query(ctx, submissionSelect+` ORDER BY s.created_at DESC LIMIT 500`)
	if err != nil {
		return nil, err
	}
	return collect(rows, func(r pgx.Rows) (*Submission, error) { return scanSubmission(r) })
}

func (s *Store) UpdateSubmission(ctx context.Context, id int64, status, adminNote, sku string) (*Submission, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE author_submissions SET status = $2, admin_note = $3, sku = $4, updated_at = now() WHERE id = $1`,
		id, status, adminNote, sku)
	if err != nil {
		return nil, mapErr(err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return scanSubmission(s.pool.QueryRow(ctx, submissionSelect+` WHERE s.id = $1`, id))
}

// ---- helpers ----

func collect[T any](rows pgx.Rows, scan func(pgx.Rows) (T, error)) ([]T, error) {
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func mapErr(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return fmt.Errorf("%w: %s", ErrConflict, pg.Detail)
	}
	return err
}
