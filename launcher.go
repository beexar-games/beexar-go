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
// catalogue, issue and cancel freespins, read the bet levels they are issued
// at.
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
	return c.launch(ctx, "/api/v1/softswiss/launcher/real", withCasinoID(req, c.casinoID))
}

// LaunchDemo starts a demo session on a virtual balance. No wallet callbacks
// are made during demo play.
func (c *Client) LaunchDemo(ctx context.Context, req LaunchDemoRequest) (LaunchResult, error) {
	return c.launch(ctx, "/api/v1/softswiss/launcher/demo", withCasinoID(req, c.casinoID))
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

// IssueFreespins is Freespins/Issue: it gives a player free rounds in the
// listed games. The player finds them on their next LaunchReal of one of those
// games; what they win arrives once, on your /freespins/finish callback.
//
// Record the issue on your side before calling this — the result names only
// issue_id. A repeat with the same terms answers like the first call, so retry
// a timeout or a 5xx with the SAME request: a ValidUntil computed again is
// other terms, and other terms under one IssueID answer 610.
func (c *Client) IssueFreespins(ctx context.Context, req FreespinsIssueRequest) error {
	// The answer is an empty object; a 200 is all there is to read.
	_, err := c.postSigned(ctx, "/api/v1/softswiss/freespins/issue", withCasinoID(req, c.casinoID))
	return err
}

// CancelFreespins is Freespins/Cancel, at any time — even mid-play. A canceled
// issue never reaches /freespins/finish: what it won is forfeited. Canceling an
// issue that has already been played out is refused with api_code 620.
func (c *Client) CancelFreespins(ctx context.Context, req FreespinsCancelRequest) error {
	_, err := c.postSigned(ctx, "/api/v1/softswiss/freespins/cancel", withCasinoID(req, c.casinoID))
	return err
}

// BetLevels is Bet/Levels: the bet levels freespins can be issued at, per game
// and currency. BetLevel in IssueFreespins is one of these.
func (c *Client) BetLevels(ctx context.Context, req BetLevelsRequest) ([]BetLevelData, error) {
	body, err := c.postSigned(ctx, "/api/v1/softswiss/bet/levels", withCasinoID(req, c.casinoID))
	if err != nil {
		return nil, err
	}
	var out struct {
		BetLevels []BetLevelData `json:"bet_levels"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("beexar: could not decode bet levels: %w", err)
	}
	return out.BetLevels, nil
}

func (c *Client) launch(ctx context.Context, path string, payload any) (LaunchResult, error) {
	body, err := c.postSigned(ctx, path, payload)
	if err != nil {
		return LaunchResult{}, err
	}
	var out LaunchResult
	if err := json.Unmarshal(body, &out); err != nil {
		return LaunchResult{}, fmt.Errorf("beexar: could not decode launch response: %w", err)
	}
	return out, nil
}

// postSigned sends one signed POST and returns the body of its 200 answer;
// any other status is an *APIError.
func (c *Client) postSigned(ctx context.Context, path string, payload any) ([]byte, error) {
	// Marshal ONCE. The signature and the request body are the same bytes, so
	// they cannot disagree.
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("beexar: could not encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set(SignatureHeader, Sign(body, c.apiSecret))

	respBody, status, err := c.do(req)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, newAPIError(status, respBody)
	}
	return respBody, nil
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
	// UseNumber keeps every number exactly as it was marshalled: decoded into
	// a float64, an integer past 2^53 would lose its low digits on the way.
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	_ = dec.Decode(&out)
	out["casino_id"] = casinoID
	return out
}
