// Package places suggests shipping addresses as people type, through the
// Google Places API (New). The server proxies it so the API key stays secret.
// Docs: https://developers.google.com/maps/documentation/places/web-service/place-autocomplete
package places

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/hackclub/merch/internal/swag"
)

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func New(baseURL, apiKey string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), apiKey: apiKey, http: hc}
}

// Suggestion is one address the person might mean.
type Suggestion struct {
	PlaceID   string `json:"place_id"`
	Main      string `json:"main"`      // "15 Falls Rd"
	Secondary string `json:"secondary"` // "Shelburne, VT, USA"
}

type text struct {
	Text string `json:"text"`
}

// Suggest returns street addresses matching input. session groups the
// keystrokes and the final Lookup into one billed session.
func (c *Client) Suggest(ctx context.Context, input, session string) ([]Suggestion, error) {
	body := map[string]any{
		"input":                input,
		"includedPrimaryTypes": []string{"street_address", "premise", "subpremise"},
	}
	if session != "" {
		body["sessionToken"] = session
	}
	var out struct {
		Suggestions []struct {
			PlacePrediction *struct {
				PlaceID          string `json:"placeId"`
				Text             text   `json:"text"`
				StructuredFormat struct {
					MainText      text `json:"mainText"`
					SecondaryText text `json:"secondaryText"`
				} `json:"structuredFormat"`
			} `json:"placePrediction"`
		} `json:"suggestions"`
	}
	if err := c.do(ctx, http.MethodPost, "/v1/places:autocomplete", "", body, &out); err != nil {
		return nil, err
	}
	res := []Suggestion{}
	for _, s := range out.Suggestions {
		p := s.PlacePrediction
		if p == nil || p.PlaceID == "" {
			continue
		}
		main := p.StructuredFormat.MainText.Text
		if main == "" {
			main = p.Text.Text
		}
		res = append(res, Suggestion{PlaceID: p.PlaceID, Main: main, Secondary: p.StructuredFormat.SecondaryText.Text})
	}
	return res, nil
}

type component struct {
	LongText  string   `json:"longText"`
	ShortText string   `json:"shortText"`
	Types     []string `json:"types"`
}

// Lookup returns the mailing address of a suggested place. Names and phone
// are left blank.
func (c *Client) Lookup(ctx context.Context, placeID, session string) (swag.Address, error) {
	q := url.Values{}
	if session != "" {
		q.Set("sessionToken", session)
	}
	var out struct {
		AddressComponents []component `json:"addressComponents"`
		PostalAddress     *struct {
			RegionCode         string   `json:"regionCode"`
			PostalCode         string   `json:"postalCode"`
			AdministrativeArea string   `json:"administrativeArea"`
			Locality           string   `json:"locality"`
			AddressLines       []string `json:"addressLines"`
		} `json:"postalAddress"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/places/"+url.PathEscape(placeID), q.Encode(), nil, &out); err != nil {
		return swag.Address{}, err
	}

	find := func(types ...string) *component {
		for _, t := range types {
			for i, c := range out.AddressComponents {
				if slices.Contains(c.Types, t) {
					return &out.AddressComponents[i]
				}
			}
		}
		return nil
	}
	long := func(types ...string) string {
		if c := find(types...); c != nil {
			return c.LongText
		}
		return ""
	}
	short := func(types ...string) string {
		if c := find(types...); c != nil {
			return c.ShortText
		}
		return ""
	}

	a := swag.Address{
		City:       long("locality", "postal_town", "sublocality_level_1", "sublocality"),
		State:      short("administrative_area_level_1"),
		PostalCode: long("postal_code"),
		Country:    short("country"),
	}
	number, street := long("street_number"), short("route")
	if numberAfterStreet[a.Country] {
		a.Line1 = strings.TrimSpace(street + " " + number)
	} else {
		a.Line1 = strings.TrimSpace(number + " " + street)
	}
	if p := out.PostalAddress; p != nil {
		if len(p.AddressLines) > 0 {
			a.Line1 = p.AddressLines[0]
			a.Line2 = strings.Join(p.AddressLines[1:], ", ")
		}
		a.City = cmp(p.Locality, a.City)
		// The component's short code ("VT"); postalAddress spells it out ("Vermont").
		a.State = cmp(a.State, p.AdministrativeArea)
		a.PostalCode = cmp(p.PostalCode, a.PostalCode)
		a.Country = cmp(p.RegionCode, a.Country)
	}
	if sub := long("subpremise"); a.Line2 == "" && sub != "" && !strings.Contains(a.Line1, sub) {
		a.Line2 = sub
	}
	a.Country = strings.ToUpper(a.Country)
	return a, nil
}

func cmp(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// Countries that write the house number after the street name.
var numberAfterStreet = map[string]bool{}

func init() {
	for _, cc := range strings.Fields("AR AT BA BE BR CH CL CZ DE DK EE ES FI GR HR HU IS IT LT LV MX NL NO PL PT RO RS SE SI SK TR UY") {
		numberAfterStreet[cc] = true
	}
}

func (c *Client) do(ctx context.Context, method, path, query string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	u := c.baseURL + path
	if query != "" {
		u += "?" + query
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("X-Goog-Api-Key", c.apiKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	} else {
		req.Header.Set("X-Goog-FieldMask", "addressComponents,postalAddress")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("places: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		json.Unmarshal(raw, &e)
		msg := e.Error.Message
		if msg == "" {
			msg = string(raw)
		}
		return fmt.Errorf("places: HTTP %d: %s", resp.StatusCode, msg)
	}
	return json.Unmarshal(raw, out)
}
