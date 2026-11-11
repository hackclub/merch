// Package slackdm sends only explicit, opted-in direct messages. The caller
// supplies the signed-in requester's verified HCA Slack identity, never a channel.
package slackdm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL, key string
	hc           *http.Client
}

func New(baseURL, key string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{strings.TrimRight(baseURL, "/"), key, hc}
}
func (c *Client) call(ctx context.Context, method string, body any, out any) error {
	b, _ := json.Marshal(body)
	r, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/"+method, bytes.NewReader(b))
	if err != nil {
		return err
	}
	r.Header.Set("Authorization", "Bearer "+c.key)
	r.Header.Set("Content-Type", "application/json")
	resp, err := c.hc.Do(r)
	if err != nil {
		return errors.New("Slack transport unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("Slack HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(out)
}
func (c *Client) Send(ctx context.Context, user, body, key string) error {
	if !strings.HasPrefix(user, "U") && !strings.HasPrefix(user, "W") {
		return errors.New("verified Slack user ID required")
	}
	for _, v := range user {
		if !(v >= 'A' && v <= 'Z' || v >= '0' && v <= '9') {
			return errors.New("invalid Slack identity")
		}
	}
	var open struct {
		OK      bool `json:"ok"`
		Channel struct {
			ID string `json:"id"`
		} `json:"channel"`
	}
	if err := c.call(ctx, "conversations.open", map[string]any{"users": user}, &open); err != nil {
		return err
	}
	if !open.OK || !strings.HasPrefix(open.Channel.ID, "D") {
		return errors.New("Slack DM unavailable")
	}
	h := sha256.Sum256([]byte(key))
	h[6] = (h[6] & 15) | 64
	h[8] = (h[8] & 63) | 128
	uuid := fmt.Sprintf("%x-%x-%x-%x-%x", h[:4], h[4:6], h[6:8], h[8:10], h[10:16])
	body = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(body)
	var sent struct {
		OK bool `json:"ok"`
	}
	if err := c.call(ctx, "chat.postMessage", map[string]any{"channel": open.Channel.ID, "text": body, "client_msg_id": uuid, "unfurl_links": false, "unfurl_media": false, "mrkdwn": false}, &sent); err != nil {
		return err
	}
	if !sent.OK {
		return errors.New("Slack refused DM")
	}
	return nil
}
