package beexar_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	beexar "github.com/beexar-games/beexar-go"
)

const testSecret = "unit-test-secret"

// A body whose key order no JSON serialiser produces. If anything parses and
// re-serialises before the SDK sees it, these bytes change and the signature
// cannot match — which is the failure this fixture exists to catch.
const hostileBody = `{"transactions":[{"type":"bet","id_provider":"tx-1","amount":"10.00"}],` +
	`"round_id":"r-1","game_id":"dice","currency":"EUR","account_id":"player_1"}`

type stubWallet struct {
	betWinErr error
}

func (s stubWallet) Balance(context.Context, beexar.BalanceRequest, beexar.RequestContext) (beexar.BalanceResult, error) {
	return beexar.BalanceResult{Balance: beexar.MustParseMoney("100.00")}, nil
}

func (s stubWallet) BetWin(_ context.Context, req beexar.BetWinRequest, _ beexar.RequestContext) (beexar.BetWinResult, error) {
	if s.betWinErr != nil {
		return beexar.BetWinResult{}, s.betWinErr
	}
	out := beexar.BetWinResult{RoundID: "own-round", Balance: beexar.MustParseMoney("90.00")}
	for _, t := range req.Transactions {
		out.Transactions = append(out.Transactions, beexar.BetWinTransactionResult{IDProvider: t.IDProvider, ID: "own-1"})
	}
	return out, nil
}

func (s stubWallet) Rollback(context.Context, beexar.RollbackRequest, beexar.RequestContext) (beexar.RollbackResult, error) {
	return beexar.RollbackResult{Balance: beexar.MustParseMoney("100.00"), RoundID: "own-round"}, nil
}

func (s stubWallet) Finish(context.Context, beexar.FinishRequest, beexar.RequestContext) (beexar.FinishResult, error) {
	return beexar.FinishResult{Balance: beexar.MustParseMoney("100.00")}, nil
}

func newServer(t *testing.T, opts ...beexar.WalletServerOption) *beexar.WalletServer {
	t.Helper()
	s, err := beexar.NewWalletServer(stubWallet{}, testSecret, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNewWalletServerRefusesAnEmptySecret(t *testing.T) {
	// The gateway treats an empty secret as an automatic failure, so a server
	// built with one would authenticate nothing while looking healthy.
	for _, secret := range []string{"", "   "} {
		if _, err := beexar.NewWalletServer(stubWallet{}, secret); err == nil {
			t.Errorf("secret %q must be refused", secret)
		}
	}
}

func TestSignatureFailureIs400WithAPICode403(t *testing.T) {
	s := newServer(t)
	out := s.Dispatch(context.Background(), beexar.RouteBalance, []byte(`{}`), strings.Repeat("0", 64), nil)
	if out.Status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 — the wallet contract never answers HTTP 403", out.Status)
	}
	var env beexar.ErrorEnvelope
	_ = json.Unmarshal(out.Body, &env)
	if env.Meta.APICode != beexar.APICodeForbidden {
		t.Errorf("api_code = %q, want 403", env.Meta.APICode)
	}
}

func TestSignatureDiagnosticNamesTheReserialisation(t *testing.T) {
	var warnings []string
	s := newServer(t, beexar.WithWarning(func(m string) { warnings = append(warnings, m) }))

	// What a body parser would hand over: json.Marshal of the parsed object.
	reserialised := `{"account_id":"p","currency":"EUR","game_id":"dice"}`
	s.Dispatch(context.Background(), beexar.RouteBalance, []byte(reserialised), strings.Repeat("0", 64), nil)
	if !strings.Contains(strings.Join(warnings, " "), "re-serialisation") {
		t.Errorf("expected a re-serialisation diagnostic, got %v", warnings)
	}

	warnings = nil
	s.Dispatch(context.Background(), beexar.RouteBalance, nil, strings.Repeat("0", 64), nil)
	if !strings.Contains(strings.Join(warnings, " "), "consumed the stream") {
		t.Errorf("expected a consumed-stream diagnostic, got %v", warnings)
	}
}

func TestValidationRunsBeforeTheHandler(t *testing.T) {
	s := newServer(t)
	cases := []struct {
		name  string
		route beexar.Route
		body  string
		want  string
	}{
		{"zero amount", beexar.RouteBetWin,
			`{"account_id":"p","currency":"EUR","game_id":"d","round_id":"r","transactions":[{"id_provider":"t","type":"bet","amount":"0"}]}`,
			"greater than zero"},
		{"lowercase currency", beexar.RouteBalance,
			`{"account_id":"p","currency":"eur","game_id":"d"}`,
			"currency"},
		{"malformed json", beexar.RouteBalance, `{`, "malformed JSON"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := s.Dispatch(context.Background(), c.route, []byte(c.body), beexar.Sign([]byte(c.body), testSecret), nil)
			if out.Status != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", out.Status)
			}
			if !strings.Contains(string(out.Body), c.want) {
				t.Errorf("body %s does not mention %q", out.Body, c.want)
			}
		})
	}
}

func TestUnknownFieldsAreTolerated(t *testing.T) {
	// additionalProperties is true on purpose, and the gateway probes operators
	// with an extra field. Strictness here breaks a valid request.
	s := newServer(t)
	body := `{"account_id":"p","currency":"EUR","game_id":"d","new_param":"probe"}`
	out := s.Dispatch(context.Background(), beexar.RouteBalance, []byte(body), beexar.Sign([]byte(body), testSecret), nil)
	if out.Status != http.StatusOK {
		t.Errorf("status = %d, want 200 (body %s)", out.Status, out.Body)
	}
}

func TestUnexpectedHandlerErrorLeaksNothing(t *testing.T) {
	s, err := beexar.NewWalletServer(stubWallet{betWinErr: errLedgerDown}, testSecret)
	if err != nil {
		t.Fatal(err)
	}
	out := s.Dispatch(context.Background(), beexar.RouteBetWin, []byte(hostileBody), beexar.Sign([]byte(hostileBody), testSecret), nil)
	if out.Status != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", out.Status)
	}
	if strings.Contains(string(out.Body), "ledger-db-7") {
		t.Errorf("internal detail leaked: %s", out.Body)
	}
}

var errLedgerDown = &ledgerDownError{}

type ledgerDownError struct{}

func (*ledgerDownError) Error() string { return "connection to ledger-db-7 refused" }

func TestFundsErrorsCannotBeBuiltWithoutABalance(t *testing.T) {
	if _, err := beexar.WalletErrorWithAPICode(beexar.APICodeInsufficientFunds, "no funds", nil); err == nil {
		t.Error("api_code 100 must require a balance")
	}
	balance := beexar.MustParseMoney("1.23")
	for _, e := range []*beexar.WalletError{
		beexar.ErrInsufficientFunds(balance),
		beexar.ErrBetLimitReached(balance),
		beexar.ErrMaxBetExceeded(balance),
	} {
		if got := e.Envelope().Meta.Balance; got != "1.23" {
			t.Errorf("api_code %s lost its balance: %q", e.APICode, got)
		}
	}
}

func TestBonusAmountDefaultsToZero(t *testing.T) {
	s := newServer(t)
	out := s.Dispatch(context.Background(), beexar.RouteBetWin, []byte(hostileBody), beexar.Sign([]byte(hostileBody), testSecret), nil)
	var body struct {
		Transactions []struct {
			BonusAmount string `json:"bonus_amount"`
		} `json:"transactions"`
	}
	if err := json.Unmarshal(out.Body, &body); err != nil {
		t.Fatal(err)
	}
	// The contract requires the field in every transaction.
	if len(body.Transactions) != 1 || body.Transactions[0].BonusAmount != "0.00" {
		t.Errorf("bonus_amount = %+v, want 0.00", body.Transactions)
	}
}

func TestHTTPHandlerServesAnyMountPath(t *testing.T) {
	// The gateway POSTs to the full callback URL from the backoffice, prefix
	// and all.
	srv := httptest.NewServer(newServer(t).NewHandler())
	defer srv.Close()

	for _, path := range []string{"/betwin", "/wallet/betwin", "/api/v2/psp/betwin"} {
		req, err := http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader(hostileBody))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(beexar.SignatureHeader, beexar.Sign([]byte(hostileBody), testSecret))

		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s: status %d", path, resp.StatusCode)
		}
	}
}

func TestHTTPHandlerRefusesAnOversizeBody(t *testing.T) {
	srv := httptest.NewServer(newServer(t).NewHandler())
	defer srv.Close()

	huge := strings.Repeat("x", beexar.MaxBodyBytes+1)
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/betwin", strings.NewReader(huge))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(beexar.SignatureHeader, beexar.Sign([]byte(huge), testSecret))

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}
