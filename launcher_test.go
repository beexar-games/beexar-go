package beexar_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	beexar "github.com/beexar-games/beexar-go"
)

const clientSecret = "launcher-test-secret"

// recordedCall is exactly what reached the gateway.
type recordedCall struct {
	path      string
	body      []byte
	signature string
}

// fakeGateway answers every call with one status and body, and records what
// went on the wire.
func fakeGateway(t *testing.T, status int, response string) (*beexar.Client, *[]recordedCall) {
	t.Helper()
	var calls []recordedCall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls = append(calls, recordedCall{path: r.URL.Path, body: body, signature: r.Header.Get(beexar.SignatureHeader)})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(srv.Close)

	client, err := beexar.NewClient("casinoxyz", clientSecret, beexar.WithBaseURL(srv.URL), beexar.WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatal(err)
	}
	return client, &calls
}

var issueRequest = beexar.FreespinsIssueRequest{
	IssueID:           "fs-1",
	Games:             []string{"dice", "plinko"},
	FreespinsQuantity: 10,
	BetLevel:          2,
	ValidUntil:        "2026-11-30T12:00:00Z",
	Account:           beexar.LaunchAccount{ID: "player_1", Currency: "EUR"},
}

func decodeBody(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("the client sent something that is not JSON: %s", body)
	}
	return out
}

func TestIssueFreespinsSignsTheExactBytesItSends(t *testing.T) {
	client, calls := fakeGateway(t, http.StatusOK, `{}`)

	if err := client.IssueFreespins(context.Background(), issueRequest); err != nil {
		t.Fatal(err)
	}

	call := (*calls)[0]
	if call.path != "/api/v1/softswiss/freespins/issue" {
		t.Errorf("path = %s", call.path)
	}
	// The signature and the body come from one serialisation, so they cannot
	// drift — this is the property that keeps the call accepted.
	if !beexar.Verify(call.body, call.signature, clientSecret) {
		t.Errorf("signature %s does not cover the body %s", call.signature, call.body)
	}
	want := map[string]any{
		"casino_id":          "casinoxyz",
		"issue_id":           "fs-1",
		"games":              []any{"dice", "plinko"},
		"freespins_quantity": float64(10),
		"bet_level":          float64(2),
		"valid_until":        "2026-11-30T12:00:00Z",
		"account":            map[string]any{"id": "player_1", "currency": "EUR"},
	}
	if got := decodeBody(t, call.body); !reflect.DeepEqual(got, want) {
		t.Errorf("body:\n got  %v\n want %v", got, want)
	}
}

func TestIssueFreespinsKeepsIntegersExact(t *testing.T) {
	// The casino_id merge goes through a map; a float64 on the way would turn
	// an integer past 2^53 into a different one. bet_level is far smaller in
	// practice, but the merge must not be where a number changes.
	client, calls := fakeGateway(t, http.StatusOK, `{}`)
	req := issueRequest
	req.BetLevel = 9007199254740993 // 2^53 + 1

	if err := client.IssueFreespins(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	var got struct {
		BetLevel json.Number `json:"bet_level"`
	}
	if err := json.Unmarshal((*calls)[0].body, &got); err != nil {
		t.Fatal(err)
	}
	if got.BetLevel != "9007199254740993" {
		t.Errorf("bet_level on the wire = %s, want 9007199254740993", got.BetLevel)
	}
}

func TestCancelFreespinsPostsTheIssueID(t *testing.T) {
	client, calls := fakeGateway(t, http.StatusOK, `{}`)

	if err := client.CancelFreespins(context.Background(), beexar.FreespinsCancelRequest{IssueID: "fs-1"}); err != nil {
		t.Fatal(err)
	}
	call := (*calls)[0]
	if call.path != "/api/v1/softswiss/freespins/cancel" {
		t.Errorf("path = %s", call.path)
	}
	want := map[string]any{"casino_id": "casinoxyz", "issue_id": "fs-1"}
	if got := decodeBody(t, call.body); !reflect.DeepEqual(got, want) {
		t.Errorf("body = %v, want %v", got, want)
	}
	if !beexar.Verify(call.body, call.signature, clientSecret) {
		t.Error("the cancel is not signed over its own bytes")
	}
}

func TestBetLevelsSendsTheFiltersAndUnwrapsTheAnswer(t *testing.T) {
	client, calls := fakeGateway(t, http.StatusOK,
		`{"bet_levels":[{"games":["dice"],"currencies":[{"currency":"EUR","bet_levels":[{"bet_level":1,"amount":"0.10"},{"bet_level":2,"amount":"0.20"}]}]}]}`)

	got, err := client.BetLevels(context.Background(), beexar.BetLevelsRequest{Games: []string{"dice"}, Currencies: []string{"EUR"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []beexar.BetLevelData{{
		Games: []string{"dice"},
		Currencies: []beexar.CurrencyBetLevels{{
			Currency:  "EUR",
			BetLevels: []beexar.BetLevel{{BetLevel: 1, Amount: "0.10"}, {BetLevel: 2, Amount: "0.20"}},
		}},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("levels:\n got  %+v\n want %+v", got, want)
	}

	call := (*calls)[0]
	if call.path != "/api/v1/softswiss/bet/levels" {
		t.Errorf("path = %s", call.path)
	}
	wantBody := map[string]any{"casino_id": "casinoxyz", "games": []any{"dice"}, "currencies": []any{"EUR"}}
	if body := decodeBody(t, call.body); !reflect.DeepEqual(body, wantBody) {
		t.Errorf("body = %v, want %v", body, wantBody)
	}
}

func TestBetLevelsWithoutFiltersAsksForEverything(t *testing.T) {
	client, calls := fakeGateway(t, http.StatusOK, `{"bet_levels":[]}`)

	if _, err := client.BetLevels(context.Background(), beexar.BetLevelsRequest{}); err != nil {
		t.Fatal(err)
	}
	// An absent filter means "all"; an empty array would say something else.
	want := map[string]any{"casino_id": "casinoxyz"}
	if body := decodeBody(t, (*calls)[0].body); !reflect.DeepEqual(body, want) {
		t.Errorf("body = %v, want %v", body, want)
	}
}

func TestFreespinsRefusalIsAnAPIErrorKeyedOnAPICode(t *testing.T) {
	client, _ := fakeGateway(t, http.StatusBadRequest,
		`{"code":"invalid_argument","msg":"issue_id fs-1 was issued with other terms: valid_until","meta":{"api_code":"610","api_message":"issue_id fs-1 was issued with other terms: valid_until"}}`)

	err := client.IssueFreespins(context.Background(), issueRequest)
	var apiErr *beexar.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want *APIError", err)
	}
	if apiErr.APICode != beexar.APICodeFreespinsInvalidIssue || apiErr.Retryable() {
		t.Errorf("got %+v, want a terminal api_code 610", apiErr)
	}
}

func TestFreespinsServerErrorIsRetryable(t *testing.T) {
	client, _ := fakeGateway(t, http.StatusInternalServerError, `{"code":"internal","meta":{"api_code":"500"}}`)

	err := client.CancelFreespins(context.Background(), beexar.FreespinsCancelRequest{IssueID: "fs-1"})
	var apiErr *beexar.APIError
	if !errors.As(err, &apiErr) || !apiErr.Retryable() {
		t.Errorf("err = %v, want a retryable *APIError", err)
	}
}
