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
	return c.send(ctx, c.http, filename, contentType, bytes.NewReader(data), int64(len(data)))
}

// UploadStream is Upload reading the file from r as it sends it, so it never
// sits in memory whole: size bytes (an exact Content-Length), or -1 to send it
// chunked. It has no overall timeout, since r may be a slow upload coming in
// from a browser; ctx bounds it. A body shorter than size fails the upload.
func (c *Client) UploadStream(ctx context.Context, filename, contentType string, r io.Reader, size int64) (string, error) {
	return c.send(ctx, &http.Client{Transport: c.http.Transport}, filename, contentType, r, size)
}

func (c *Client) send(ctx context.Context, hc *http.Client, filename, contentType string, r io.Reader, size int64) (string, error) {
	// The multipart framing around the file, written up front so the file itself streams.
	var frame bytes.Buffer
	mw := multipart.NewWriter(&frame)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, filename))
	h.Set("Content-Type", contentType)
	if _, err := mw.CreatePart(h); err != nil {
		return "", err
	}
	head := bytes.Clone(frame.Bytes())
	frame.Reset()
	if err := mw.Close(); err != nil {
		return "", err
	}
	tail := frame.Bytes()

	if size >= 0 {
		r = &exact{r: r, left: size}
	}
	body := io.MultiReader(bytes.NewReader(head), r, bytes.NewReader(tail))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v4/upload", body)
	if err != nil {
		return "", err
	}
	req.ContentLength = -1
	if size >= 0 {
		req.ContentLength = int64(len(head)) + size + int64(len(tail))
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := hc.Do(req)
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

// exact reads r, failing if it ends before left bytes: the closing multipart
// boundary must never follow a cut-off file, or the CDN would store it whole.
type exact struct {
	r    io.Reader
	left int64
}

func (e *exact) Read(p []byte) (int, error) {
	if e.left <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > e.left {
		p = p[:e.left]
	}
	n, err := e.r.Read(p)
	e.left -= int64(n)
	if err == io.EOF && e.left > 0 {
		err = io.ErrUnexpectedEOF
	}
	return n, err
}
