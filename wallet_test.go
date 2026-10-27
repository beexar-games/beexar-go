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

func TestVerifyRefusesMalformedSignatures(t *testing.T) {
	// Parity with the Node and Python SDKs: a header that is not 64 lowercase
	// hex characters is answered false, never with a panic or an error. The
	// non-ASCII rows are the shape that crashed the other two.
	body := []byte(`{}`)
	cases := []struct {
		name      string
		signature string
	}{
		{"64 non-ASCII chars", strings.Repeat("é", 64)},
		{"63 non-ASCII chars", strings.Repeat("é", 63)},
		{"empty", ""},
		{"wrong length", "deadbeef"},
		{"wrong value", strings.Repeat("0", 64)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if beexar.Verify(body, c.signature, testSecret) {
				t.Errorf("Verify(%q) = true, want false", c.signature)
			}
		})
	}
	if !beexar.Verify(body, beexar.Sign(body, testSecret), testSecret) {
		t.Error("Verify rejects the signature Sign produced for the same bytes")
	}
}

func TestNonASCIISignatureIs400WithAPICode403(t *testing.T) {
	s := newServer(t, beexar.WithWarning(func(string) {}))
	out := s.Dispatch(context.Background(), beexar.RouteBalance, []byte(`{}`), strings.Repeat("é", 64), nil)
	if out.Status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 — a malformed header is a signature refusal, not a 500", out.Status)
	}
	var env beexar.ErrorEnvelope
	if err := json.Unmarshal(out.Body, &env); err != nil {
		t.Fatalf("body is not the error envelope: %v (%s)", err, out.Body)
	}
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

// freespinsWallet is stubWallet plus the optional fifth method.
type freespinsWallet struct {
	stubWallet
	seen *beexar.FreespinsFinishRequest
}

func (f *freespinsWallet) FreespinsFinish(_ context.Context, req beexar.FreespinsFinishRequest, _ beexar.RequestContext) (beexar.FreespinsFinishResult, error) {
	f.seen = &req
	return beexar.FreespinsFinishResult{Balance: beexar.MustParseMoney("112.50")}, nil
}

const freespinsFinishBody = `{"issue_id":"fs-1","amount":"12.50"}`

func dispatchSigned(s *beexar.WalletServer, route beexar.Route, body string) beexar.DispatchResponse {
	return s.Dispatch(context.Background(), route, []byte(body), beexar.Sign([]byte(body), testSecret), nil)
}

func TestFreespinsFinishReachesTheOptionalHandler(t *testing.T) {
	wallet := &freespinsWallet{}
	s, err := beexar.NewWalletServer(wallet, testSecret)
	if err != nil {
		t.Fatal(err)
	}

	out := dispatchSigned(s, beexar.RouteFreespinsFinish, freespinsFinishBody)
	if out.Status != http.StatusOK || string(out.Body) != `{"balance":"112.50"}` {
		t.Fatalf("got %d %s, want 200 {\"balance\":\"112.50\"}", out.Status, out.Body)
	}
	if wallet.seen == nil || wallet.seen.IssueID != "fs-1" || wallet.seen.Amount.String() != "12.50" {
		t.Errorf("handler saw %+v", wallet.seen)
	}

	// Zero is a result here, not a bad request: the freespins won nothing.
	out = dispatchSigned(s, beexar.RouteFreespinsFinish, `{"issue_id":"fs-1","amount":"0"}`)
	if out.Status != http.StatusOK || !wallet.seen.Amount.IsZero() {
		t.Errorf("zero amount: status %d, handler saw %+v", out.Status, wallet.seen)
	}
}

func TestFreespinsFinishIsValidatedBeforeTheHandler(t *testing.T) {
	cases := []struct {
		name, body, want string
	}{
		{"negative amount", `{"issue_id":"fs-1","amount":"-1.00"}`, "amount: not a valid decimal amount"},
		{"no amount", `{"issue_id":"fs-1"}`, "amount: not a valid decimal amount"},
		{"no issue_id", `{"amount":"12.50"}`, "issue_id: required, must be a non-empty string"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wallet := &freespinsWallet{}
			s, err := beexar.NewWalletServer(wallet, testSecret)
			if err != nil {
				t.Fatal(err)
			}
			out := dispatchSigned(s, beexar.RouteFreespinsFinish, c.body)
			var env beexar.ErrorEnvelope
			_ = json.Unmarshal(out.Body, &env)
			if out.Status != http.StatusBadRequest || env.Msg != c.want {
				t.Errorf("got %d %q, want 400 %q", out.Status, env.Msg, c.want)
			}
			if wallet.seen != nil {
				t.Error("the handler ran on an invalid request")
			}
		})
	}
}

func TestFreespinsFinishWithoutTheHandlerAnswersLikeAnUnknownRoute(t *testing.T) {
	// stubWallet has the four methods only — a wallet written before freespins
	// existed, or one that does not offer them. It must keep compiling and
	// serving, and answer /freespins/finish the way an unknown route is.
	s := newServer(t)

	out := dispatchSigned(s, beexar.RouteFreespinsFinish, freespinsFinishBody)
	var env beexar.ErrorEnvelope
	if err := json.Unmarshal(out.Body, &env); err != nil {
		t.Fatal(err)
	}
	if out.Status != http.StatusBadRequest || env.Meta.APICode != beexar.APICodeNotFound ||
		env.Msg != "freespins finish is not implemented by this wallet" {
		t.Errorf("got %d %+v", out.Status, env)
	}
	if out := dispatchSigned(s, beexar.RouteBalance, `{"account_id":"p","currency":"EUR","game_id":"d"}`); out.Status != http.StatusOK {
		t.Errorf("/balance stopped working: %d", out.Status)
	}
}

func TestFreespinsRefusalsCarryTheirAPICodes(t *testing.T) {
	if got := beexar.ErrInvalidFreespinsIssue("").Envelope().Meta.APICode; got != "610" {
		t.Errorf("ErrInvalidFreespinsIssue api_code = %s, want 610", got)
	}
	if got := beexar.ErrFreespinsStateTransition("").Envelope().Meta.APICode; got != "605" {
		t.Errorf("ErrFreespinsStateTransition api_code = %s, want 605", got)
	}
}

func TestRouteFromPathPrefersTheLongestRoute(t *testing.T) {
	// /freespins/finish ends with /finish too. Matching the shorter route first
	// would hand a freespins result to the round's finish handler.
	cases := map[string]beexar.Route{
		"/freespins/finish":         beexar.RouteFreespinsFinish,
		"/x/freespins/finish":       beexar.RouteFreespinsFinish,
		"/wallet/freespins/finish/": beexar.RouteFreespinsFinish,
		"/finish":                   beexar.RouteFinish,
		"/x/finish":                 beexar.RouteFinish,
		"/wallet/betwin":            beexar.RouteBetWin,
	}
	for path, want := range cases {
		if got, ok := beexar.RouteFromPath(path); !ok || got != want {
			t.Errorf("RouteFromPath(%q) = %q, %v; want %q", path, got, ok, want)
		}
	}
	for _, path := range []string{"/health", "/freespins"} {
		if got, ok := beexar.RouteFromPath(path); ok {
			t.Errorf("RouteFromPath(%q) = %q, want no route", path, got)
		}
	}
}

func TestHTTPHandlerTellsTheFreespinsResultFromTheRoundFinish(t *testing.T) {
	s, err := beexar.NewWalletServer(&freespinsWallet{}, testSecret)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(s.NewHandler())
	defer srv.Close()

	roundFinish := `{"account_id":"player_1","currency":"EUR","round_id":"r-1"}`
	for path, c := range map[string]struct{ body, balance string }{
		"/wallet/freespins/finish": {freespinsFinishBody, "112.50"},
		"/wallet/finish":           {roundFinish, "100.00"},
	} {
		req, err := http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader(c.body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set(beexar.SignatureHeader, beexar.Sign([]byte(c.body), testSecret))
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			Balance string `json:"balance"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&got)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK || got.Balance != c.balance {
			t.Errorf("%s: status %d balance %q, want 200 %q", path, resp.StatusCode, got.Balance, c.balance)
		}
	}
}
