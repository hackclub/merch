// Package loops sends transactional emails through Loops (loops.so). Every
// email uses one template whose variables are the subject, preview text and
// body, so the wording lives here in code.
// Docs: https://loops.so/docs/api-reference/send-transactional-email
package loops

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL         string
	key             string
	transactionalID string
	http            *http.Client
}

func New(baseURL, key, transactionalID string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), key: key, transactionalID: transactionalID, http: hc}
}

// Email is one message to one person.
type Email struct {
	To             string
	Subject        string
	Preview        string // the inbox's preview line
	Body           string // plain text; blank lines separate paragraphs (sent as HTML)
	IdempotencyKey string // Loops sends each key once (for 24 hours)
}

// bodyHTML is the template's body_text: Loops inserts it as HTML and folds
// newlines, so the text is escaped and each newline becomes a <br>.
func bodyHTML(text string) string {
	return strings.ReplaceAll(html.EscapeString(text), "\n", "<br>")
}

func (c *Client) Send(ctx context.Context, e Email) error {
	body, _ := json.Marshal(map[string]any{
		"transactionalId": c.transactionalID,
		"email":           e.To,
		"dataVariables": map[string]string{
			"subject_line": e.Subject,
			"preview_text": e.Preview,
			"body_text":    bodyHTML(e.Body),
		},
	})
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/api/v1/transactional", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Content-Type", "application/json")
	if e.IdempotencyKey != "" {
		req.Header.Set("Idempotency-Key", e.IdempotencyKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("loops: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	// 409: this idempotency key was already sent.
	if resp.StatusCode/100 == 2 || resp.StatusCode == http.StatusConflict {
		return nil
	}
	var r struct {
		Message string `json:"message"`
	}
	json.Unmarshal(data, &r)
	return fmt.Errorf("loops: %d: %s", resp.StatusCode, r.Message)
}
