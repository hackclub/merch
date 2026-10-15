// Package asendia looks up when Asendia delivered a package, through the same
// API as Asendia's public tracking page (a1.asendiausa.com/tracking). Asendia
// carries our international parcels; delivery dates feed shipping estimates.
package asendia

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sync"
	"time"
)

// PageScript is the tracking page's script. It carries the API's address and
// the public keys the page sends, which we read from it instead of storing.
const PageScript = "https://a1.asendiausa.com/tracking/js/main.js"

type Client struct {
	HTTP      *http.Client
	ScriptURL string // defaults to PageScript

	once sync.Once
	cfg  config
	err  error
}

type config struct{ baseURL, auth, apiKey, trackingKey string }

var (
	reBase  = regexp.MustCompile(`"baseUrl":\s*"([^"]+)"`)
	reAuth  = regexp.MustCompile(`"authorizationHeaderValue":\s*"([^"]+)"`)
	reKey   = regexp.MustCompile(`"a1ApiKeyHeaderValue":\s*"([^"]+)"`)
	reTrack = regexp.MustCompile(`location\.href,\w+="([0-9A-Fa-f-]{36})"`)

	// Delivery events, which each last-mile carrier codes its own way:
	// Asendia's DELIVERY, the postal (UPU) I0, Australia's FD01, or just a
	// description like "Delivered" or "Parcel delivered".
	deliveryCodes = map[string]bool{"DELIVERY": true, "I0": true, "FD01": true}
	reDelivered   = regexp.MustCompile(`(?i)^(\w+ )?delivered\b`)
)

func (c *Client) config(ctx context.Context) (config, error) {
	c.once.Do(func() {
		src := c.ScriptURL
		if src == "" {
			src = PageScript
		}
		var body []byte
		if body, c.err = c.get(ctx, src, nil); c.err != nil {
			return
		}
		for _, f := range []struct {
			re  *regexp.Regexp
			dst *string
		}{{reBase, &c.cfg.baseURL}, {reAuth, &c.cfg.auth}, {reKey, &c.cfg.apiKey}, {reTrack, &c.cfg.trackingKey}} {
			m := f.re.FindSubmatch(body)
			if m == nil {
				c.err = fmt.Errorf("asendia: tracking page script changed; can't find %s", f.re)
				return
			}
			*f.dst = string(m[1])
		}
	})
	return c.cfg, c.err
}

func (c *Client) get(ctx context.Context, u string, hdr map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("asendia: %s: %s", u, resp.Status)
	}
	return b, nil
}

// Delivered returns when the package was delivered (its earliest delivery
// event), and false if it hasn't been yet or Asendia doesn't know it.
func (c *Client) Delivered(ctx context.Context, tracking string) (time.Time, bool, error) {
	cfg, err := c.config(ctx)
	if err != nil {
		return time.Time{}, false, err
	}
	q := url.Values{"trackingKey": {cfg.trackingKey}, "trackingNumber": {tracking}}
	body, err := c.get(ctx, cfg.baseURL+"/api/A1/TrackingBranded/Tracking?"+q.Encode(), map[string]string{
		"Authorization": cfg.auth, "X-AsendiaOne-ApiKey": cfg.apiKey, "Accept": "application/json",
	})
	if err != nil {
		return time.Time{}, false, err
	}
	var out struct {
		Detail []struct {
			EventCode   string    `json:"eventCode"`
			Description string    `json:"eventDescription"`
			EventOn     time.Time `json:"eventOn"`
		} `json:"trackingBrandedDetail"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return time.Time{}, false, fmt.Errorf("asendia: %s: %w", tracking, err)
	}
	var at time.Time
	for _, e := range out.Detail {
		delivered := deliveryCodes[e.EventCode] || reDelivered.MatchString(e.Description)
		if delivered && (at.IsZero() || e.EventOn.Before(at)) {
			at = e.EventOn
		}
	}
	return at, !at.IsZero(), nil
}
