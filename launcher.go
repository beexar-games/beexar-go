package beexar

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ProductionBaseURL is the Beexar gateway.
const ProductionBaseURL = "https://gateway.beexar.com"

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithBaseURL points the client at a different gateway (staging, or a local
// stack).
func WithBaseURL(u string) ClientOption {
	return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") }
}

// WithHTTPClient supplies the HTTP client to use, for timeouts, proxies or
// tests.
func WithHTTPClient(h *http.Client) ClientOption {
	return func(c *Client) { c.http = h }
}

// Client makes the calls you send to Beexar: launch a session, list the
// catalogue.
//
// Every POST is signed with hex(HMAC-SHA256(body, apiSecret)) over the exact
// bytes that go on the wire — the body is marshalled once, signed, and sent.
// Marshalling twice is how signatures mysteriously stop matching.
type Client struct {
	casinoID  string
	apiSecret string
	baseURL   string
	http      *http.Client
}

// NewClient builds a client. casinoID is your operator slug; apiSecret comes
// from the backoffice and must never reach a browser.
func NewClient(casinoID, apiSecret string, opts ...ClientOption) (*Client, error) {
	if casinoID == "" {
		return nil, errors.New("beexar: casinoID is required")
	}
	if apiSecret == "" {
		return nil, errors.New("beexar: apiSecret is required")
	}
	c := &Client{
		casinoID:  casinoID,
		apiSecret: apiSecret,
		baseURL:   ProductionBaseURL,
		http:      &http.Client{Timeout: 10 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	return c, nil
}

// LaunchReal starts a real-money session and returns the URL to embed.
func (c *Client) LaunchReal(ctx context.Context, req LaunchRealRequest) (LaunchResult, error) {
	return c.postSigned(ctx, "/api/v1/softswiss/launcher/real", withCasinoID(req, c.casinoID))
}

// LaunchDemo starts a demo session on a virtual balance. No wallet callbacks
// are made during demo play.
func (c *Client) LaunchDemo(ctx context.Context, req LaunchDemoRequest) (LaunchResult, error) {
	return c.postSigned(ctx, "/api/v1/softswiss/launcher/demo", withCasinoID(req, c.casinoID))
}

// ListGames returns the catalogue enabled for the operator. This endpoint is
// public and carries no signature.
func (c *Client) ListGames(ctx context.Context, q ListGamesQuery) ([]GameInfo, error) {
	operator := q.Operator
	if operator == "" {
		operator = c.casinoID
	}
	params := url.Values{"operator": []string{operator}}
	if q.Active != nil {
		params.Set("active", strconv.FormatBool(*q.Active))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/operator/games?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	body, status, err := c.do(req)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, newAPIError(status, body)
	}
	var out struct {
		Games []GameInfo `json:"games"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("beexar: could not decode catalogue: %w", err)
	}
	return out.Games, nil
}

func (c *Client) postSigned(ctx context.Context, path string, payload any) (LaunchResult, error) {
	// Marshal ONCE. The signature and the request body are the same bytes, so
	// they cannot disagree.
	body, err := json.Marshal(payload)
	if err != nil {
		return LaunchResult{}, fmt.Errorf("beexar: could not encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return LaunchResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set(SignatureHeader, Sign(body, c.apiSecret))

	respBody, status, err := c.do(req)
	if err != nil {
		return LaunchResult{}, err
	}
	if status != http.StatusOK {
		return LaunchResult{}, newAPIError(status, respBody)
	}
	var out LaunchResult
	if err := json.Unmarshal(respBody, &out); err != nil {
		return LaunchResult{}, fmt.Errorf("beexar: could not decode launch response: %w", err)
	}
	return out, nil
}

func (c *Client) do(req *http.Request) ([]byte, int, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("beexar: request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("beexar: could not read response: %w", err)
	}
	return body, resp.StatusCode, nil
}

// withCasinoID merges the operator slug into the request body without making
// the caller repeat it on every call.
func withCasinoID(req any, casinoID string) map[string]any {
	raw, err := json.Marshal(req)
	if err != nil {
		return map[string]any{"casino_id": casinoID}
	}
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	out["casino_id"] = casinoID
	return out
}
