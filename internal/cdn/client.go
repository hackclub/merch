// Package cdn uploads files to cdn.hackclub.com (API v4). Uploads are
// permanent and public; we use it for catalog item images.
// Spec: https://cdn.hackclub.com/openapi.json
package cdn

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"time"
)

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func New(baseURL, apiKey string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	return &Client{baseURL: baseURL, apiKey: apiKey, http: hc}
}

// Upload stores data as filename and returns its permanent public URL.
func (c *Client) Upload(ctx context.Context, filename, contentType string, data []byte) (string, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, filename))
	h.Set("Content-Type", contentType)
	part, err := mw.CreatePart(h)
	if err != nil {
		return "", err
	}
	part.Write(data)
	if err := mw.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v4/upload", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("cdn upload: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var out struct {
		URL     string `json:"url"`
		Message string `json:"message"`
	}
	json.Unmarshal(raw, &out)
	if resp.StatusCode >= 300 {
		msg := out.Message
		if msg == "" {
			msg = string(raw)
		}
		return "", fmt.Errorf("cdn upload: HTTP %d: %s", resp.StatusCode, msg)
	}
	if out.URL == "" {
		return "", fmt.Errorf("cdn upload: response had no url")
	}
	return out.URL, nil
}
