// Package nsclient talks to the NS (Nederlandse Spoorwegen) Reisinformatie
// API: Dutch train stations, departures, trips and disruptions. Every
// request needs a subscription key from apiportal.ns.nl (product "Ns-App").
package nsclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/henrrrik/ovapi-mcp-server/ovapiclient"
)

// DefaultBase is the production gateway for the Reisinformatie API.
const DefaultBase = "https://gateway.apiportal.ns.nl/reisinformatie-api"

const maxResponseSize = 10 * 1024 * 1024 // the full station list is ~375 KB

// Client performs authenticated GETs against the NS gateway.
type Client struct {
	Base string
	key  string
	http ovapiclient.HTTPDoer
}

// New returns a client that sends key with every request.
func New(key string, httpc ovapiclient.HTTPDoer) *Client {
	return &Client{Base: DefaultBase, key: key, http: httpc}
}

// NewHTTPClient is the HTTP client to use for NS: unlike OVapi, the gateway
// has a valid certificate, so this one verifies TLS.
func NewHTTPClient() ovapiclient.HTTPDoer {
	return &http.Client{Timeout: 15 * time.Second}
}

// APIError is a non-2xx answer from the gateway. NS uses RFC 7807 problem
// documents, whose "detail" is the useful part ("Station not found: XXXX");
// other bodies (the gateway's own 401) carry a "message".
type APIError struct {
	Status int
	Detail string
}

func (e *APIError) Error() string {
	if e.Detail == "" {
		return fmt.Sprintf("NS API returned HTTP %d", e.Status)
	}
	return fmt.Sprintf("NS API returned HTTP %d: %s", e.Status, e.Detail)
}

// Get performs a GET on path (relative to Base) with params and returns the
// body. A non-2xx status is returned as *APIError.
func (c *Client) Get(ctx context.Context, path string, params url.Values) ([]byte, error) {
	if c.key == "" {
		return nil, errors.New("NS API key is not configured")
	}
	u := c.Base + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Ocp-Apim-Subscription-Key", c.key)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{Status: resp.StatusCode, Detail: problemDetail(body)}
	}
	return body, nil
}

// problemDetail extracts the human-readable part of an error body.
func problemDetail(body []byte) string {
	var p struct {
		Detail  string `json:"detail"`
		Title   string `json:"title"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return strings.TrimSpace(string(body))
	}
	for _, s := range []string{p.Detail, p.Message, p.Title} {
		if s != "" {
			return s
		}
	}
	return ""
}
