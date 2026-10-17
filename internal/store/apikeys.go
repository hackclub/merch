package store

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hackclub/merch/internal/swag"
)

// ---- API keys (agents reviewing requests) ----

// APIKey lets an agent call /api/agent/* for the admin who made it.
type APIKey struct {
	ID             int64      `json:"id"`
	Name           string     `json:"name"`
	Prefix         string     `json:"prefix"` // the token's first characters, to tell keys apart
	CreatedBy      int64      `json:"created_by"`
	CreatedByEmail string     `json:"created_by_email"`
	CreatedAt      time.Time  `json:"created_at"`
	LastUsedAt     *time.Time `json:"last_used_at"`
	RevokedAt      *time.Time `json:"revoked_at"`
}

const apiKeySelect = `
	SELECT k.id, k.name, k.prefix, k.created_by, u.email, k.created_at, k.last_used_at, k.revoked_at
	FROM api_keys k JOIN users u ON u.id = k.created_by`

func scanAPIKey(row pgx.Row) (APIKey, error) {
	var k APIKey
	err := row.Scan(&k.ID, &k.Name, &k.Prefix, &k.CreatedBy, &k.CreatedByEmail, &k.CreatedAt, &k.LastUsedAt, &k.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return k, ErrNotFound
	}
	return k, err
}

// CreateAPIKey returns the new key and its token, which is never stored or shown again.
func (s *Store) CreateAPIKey(ctx context.Context, name string, createdBy int64) (APIKey, string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return APIKey{}, "", err
	}
	tok := "merch_" + base64.RawURLEncoding.EncodeToString(b)
	var id int64
	if err := s.pool.QueryRow(ctx, `INSERT INTO api_keys (name, prefix, token_hash, created_by) VALUES ($1, $2, $3, $4) RETURNING id`,
		name, tok[:12], hashToken(tok), createdBy).Scan(&id); err != nil {
		return APIKey{}, "", err
	}
	k, err := scanAPIKey(s.pool.QueryRow(ctx, apiKeySelect+` WHERE k.id = $1`, id))
	return k, tok, err
}

func (s *Store) ListAPIKeys(ctx context.Context) ([]APIKey, error) {
	rows, err := s.pool.Query(ctx, apiKeySelect+` ORDER BY k.revoked_at IS NOT NULL, k.created_at DESC`)
	if err != nil {
		return nil, err
	}
	return collect(rows, func(r pgx.Rows) (APIKey, error) { return scanAPIKey(r) })
}

func (s *Store) RevokeAPIKey(ctx context.Context, id int64) (APIKey, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE api_keys SET revoked_at = coalesce(revoked_at, now()) WHERE id = $1`, id)
	if err != nil {
		return APIKey{}, err
	}
	if tag.RowsAffected() == 0 {
		return APIKey{}, ErrNotFound
	}
	return scanAPIKey(s.pool.QueryRow(ctx, apiKeySelect+` WHERE k.id = $1`, id))
}

// APIKeyByToken finds a live key and the admin it acts for, and notes that it was used.
func (s *Store) APIKeyByToken(ctx context.Context, tok string) (*APIKey, *User, error) {
	var id, owner int64
	err := s.pool.QueryRow(ctx, `UPDATE api_keys SET last_used_at = now() WHERE token_hash = $1 AND revoked_at IS NULL RETURNING id, created_by`,
		hashToken(tok)).Scan(&id, &owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ErrNotFound
	} else if err != nil {
		return nil, nil, err
	}
	k, err := scanAPIKey(s.pool.QueryRow(ctx, apiKeySelect+` WHERE k.id = $1`, id))
	if err != nil {
		return nil, nil, err
	}
	u, err := scanUser(s.pool.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE id = $1`, owner))
	return &k, u, err
}

// ---- what a reviewer looks at ----

// Requester is who made a request, for reviewing it.
type Requester struct {
	User
	CreatedAt   time.Time `json:"created_at"`    // first sign-in (or import)
	LastLoginAt time.Time `json:"last_login_at"` //
	Imported    bool      `json:"imported"`      // known only from Fillout-era imports; never signed in here
}

func (s *Store) GetRequester(ctx context.Context, userID int64) (*Requester, error) {
	var p Requester
	u := &p.User
	err := s.pool.QueryRow(ctx, `SELECT `+userCols+`, created_at, last_login_at, hca_id IS NULL FROM users WHERE id = $1`, userID).
		Scan(&u.ID, &u.HCAID, &u.Email, &u.Name, &u.SlackID, &u.VerificationStatus, &u.Address, &p.CreatedAt, &p.LastLoginAt, &p.Imported)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &p, err
}

// addrKey matches addresses written differently: letters and digits only, any case.
const addrKey = `lower(regexp_replace(%s, '[^[:alnum:]]', '', 'g'))`

// RequestsToAddress lists other people's requests to the same street address,
// postal code and country (newest first, at most 50).
func (s *Store) RequestsToAddress(ctx context.Context, a swag.Address, exceptUser int64) ([]*Request, error) {
	norm := func(col string) string { return strings.Replace(addrKey, "%s", col, 1) }
	return s.listRequests(ctx, requestSelect+` WHERE r.user_id <> $1 AND r.country = $2
		AND `+norm("r.line_1")+` = `+norm("$3")+` AND `+norm("r.postal_code")+` = `+norm("$4")+`
		ORDER BY r.created_at DESC LIMIT 50`, exceptUser, a.Country, a.Line1, a.PostalCode)
}

// AppendInternalNote adds a paragraph to the admins' note on a request.
func (s *Store) AppendInternalNote(ctx context.Context, id int64, text string) (*Request, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE swag_requests SET updated_at = now(),
		internal_note = CASE WHEN internal_note = '' THEN $2 ELSE internal_note || E'\n\n' || $2 END WHERE id = $1`, id, text)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return s.GetRequest(ctx, id)
}
