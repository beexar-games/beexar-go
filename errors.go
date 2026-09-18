package beexar

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// SoftSwiss API codes, ported verbatim from the platform's own registry.
//
// Two things about this contract surprise everyone once:
//
//  1. There are only two HTTP statuses (400 and 500) and two Twirp codes
//     ("invalid_argument" and "internal"). All meaning lives in meta.api_code.
//     Branch on that, never on the status.
//  2. A signature failure is HTTP 400 with api_code "403", not HTTP 403.
//     Answering 403 breaks the contract.
const (
	APICodeInsufficientFunds  = "100"
	APICodeInvalidPlayer      = "101"
	APICodeBetLimitReached    = "105"
	APICodeMaxBetExceeded     = "106"
	APICodeGameForbidden      = "107"
	APICodePlayerDisabled     = "110"
	APICodeCountryRestricted  = "153"
	APICodeCurrencyNotAllowed = "154"
	APICodeFieldImmutable     = "155"

	APICodeBadRequest = "400"
	APICodeForbidden  = "403"
	APICodeNotFound   = "404"
	// APICodeAlreadyRolledBack is a Beexar extension. Return it from /betwin to
	// reject a transaction whose id_provider was already rolled back —
	// including a rollback that arrived BEFORE the bet it reverses.
	APICodeAlreadyRolledBack = "409"

	APICodeGameNotAvailable = "405"
	APICodeCasinoDisabled   = "410"

	APICodeUnknownError       = "500"
	APICodeServiceUnavailable = "503"
	APICodeRequestTimeout     = "504"
)

// Twirp codes. Only these two exist on this contract.
const (
	TwirpCodeInvalidArgument = "invalid_argument"
	TwirpCodeInternal        = "internal"
)

// IsFundsRelatedCode reports whether an api_code must carry the player's
// balance in meta.balance.
//
// The platform will not complain if you omit it — it reads the field with the
// error swallowed — so the player simply sees a wrong balance. That is exactly
// why this SDK refuses to build such an error without one.
func IsFundsRelatedCode(code string) bool {
	return code == APICodeInsufficientFunds ||
		code == APICodeBetLimitReached ||
		code == APICodeMaxBetExceeded
}

// ErrorMeta is the meta object of the Twirp error envelope.
type ErrorMeta struct {
	APICode    string `json:"api_code"`
	APIMessage string `json:"api_message"`
	Balance    string `json:"balance,omitempty"`
}

// ErrorEnvelope is the error body both sides of this contract exchange.
type ErrorEnvelope struct {
	Code string    `json:"code"`
	Msg  string    `json:"msg"`
	Meta ErrorMeta `json:"meta"`
}

// WalletError is the error your handler returns to answer the platform with a
// specific api_code. Anything else you return becomes an opaque 500 — your
// message is never echoed to the platform.
type WalletError struct {
	Status    int
	TwirpCode string
	APICode   string
	Message   string
	// Balance is required for api_code 100, 105 and 106 and must be nil
	// otherwise.
	Balance *Money
}

// Error implements error.
func (e *WalletError) Error() string {
	return fmt.Sprintf("beexar: wallet error api_code=%s: %s", e.APICode, e.Message)
}

// Envelope renders the wire form. It panics if a funds-related code lost its
// balance on the way, which can only happen through WalletErrorWithAPICode.
func (e *WalletError) Envelope() ErrorEnvelope {
	if IsFundsRelatedCode(e.APICode) && e.Balance == nil {
		panic("beexar: api_code " + e.APICode + " must carry meta.balance")
	}
	env := ErrorEnvelope{
		Code: e.TwirpCode,
		Msg:  e.Message,
		Meta: ErrorMeta{APICode: e.APICode, APIMessage: e.Message},
	}
	if e.Balance != nil {
		env.Meta.Balance = e.Balance.String()
	}
	return env
}

func clientError(apiCode, message string, balance *Money) *WalletError {
	return &WalletError{
		Status:    http.StatusBadRequest,
		TwirpCode: TwirpCodeInvalidArgument,
		APICode:   apiCode,
		Message:   message,
		Balance:   balance,
	}
}

func serverError(apiCode, message string) *WalletError {
	return &WalletError{
		Status:    http.StatusInternalServerError,
		TwirpCode: TwirpCodeInternal,
		APICode:   apiCode,
		Message:   message,
	}
}

// --- funds-related: the balance is a required parameter, by design ---------

// ErrInsufficientFunds builds api_code 100.
func ErrInsufficientFunds(balance Money) *WalletError {
	return clientError(APICodeInsufficientFunds, "insufficient funds", &balance)
}

// ErrBetLimitReached builds api_code 105.
func ErrBetLimitReached(balance Money) *WalletError {
	return clientError(APICodeBetLimitReached, "bet limit reached", &balance)
}

// ErrMaxBetExceeded builds api_code 106.
func ErrMaxBetExceeded(balance Money) *WalletError {
	return clientError(APICodeMaxBetExceeded, "max bet exceeded", &balance)
}

// --- player / request -----------------------------------------------------

// ErrInvalidPlayer builds api_code 101.
func ErrInvalidPlayer(message string) *WalletError {
	return clientError(APICodeInvalidPlayer, orDefault(message, "invalid player"), nil)
}

// ErrPlayerDisabled builds api_code 110.
func ErrPlayerDisabled(message string) *WalletError {
	return clientError(APICodePlayerDisabled, orDefault(message, "player is disabled"), nil)
}

// ErrGameForbidden builds api_code 107.
func ErrGameForbidden(message string) *WalletError {
	return clientError(APICodeGameForbidden, orDefault(message, "game is forbidden to the player"), nil)
}

// ErrCurrencyNotAllowed builds api_code 154.
func ErrCurrencyNotAllowed(message string) *WalletError {
	return clientError(APICodeCurrencyNotAllowed, orDefault(message, "currency is not allowed for the player"), nil)
}

// ErrBadRequest builds api_code 400.
func ErrBadRequest(message string) *WalletError {
	return clientError(APICodeBadRequest, orDefault(message, "bad request"), nil)
}

// ErrInvalidSignature builds api_code 403 — with HTTP 400, as the contract
// requires.
func ErrInvalidSignature() *WalletError {
	return clientError(APICodeForbidden, "invalid signature", nil)
}

// ErrNotFound builds api_code 404.
func ErrNotFound(message string) *WalletError {
	return clientError(APICodeNotFound, orDefault(message, "not found"), nil)
}

// ErrAlreadyRolledBack builds api_code 409 — the transaction's id_provider was
// already rolled back (tombstoned).
func ErrAlreadyRolledBack() *WalletError {
	return clientError(APICodeAlreadyRolledBack, "action already rolled back", nil)
}

// --- server ---------------------------------------------------------------

// ErrInternal builds api_code 500.
func ErrInternal(message string) *WalletError {
	return serverError(APICodeUnknownError, orDefault(message, "internal error"))
}

// ErrServiceUnavailable builds api_code 503.
func ErrServiceUnavailable(message string) *WalletError {
	return serverError(APICodeServiceUnavailable, orDefault(message, "service unavailable"))
}

// WalletErrorWithAPICode is the escape hatch for an api_code without a
// dedicated constructor. It enforces the same balance rule, so it cannot be
// used to bypass it.
func WalletErrorWithAPICode(apiCode, message string, balance *Money) (*WalletError, error) {
	if IsFundsRelatedCode(apiCode) && balance == nil {
		return nil, fmt.Errorf("beexar: api_code %s must carry the player's balance", apiCode)
	}
	status := http.StatusBadRequest
	twirp := TwirpCodeInvalidArgument
	if len(apiCode) > 0 && apiCode[0] == '5' {
		status = http.StatusInternalServerError
		twirp = TwirpCodeInternal
	}
	return &WalletError{Status: status, TwirpCode: twirp, APICode: apiCode, Message: message, Balance: balance}, nil
}

func orDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// APIError is an error answer from the Beexar gateway to one of YOUR calls
// (LaunchReal, LaunchDemo, ListGames).
type APIError struct {
	HTTPStatus int
	Code       string
	APICode    string
	APIMessage string
}

// Error implements error.
func (e *APIError) Error() string {
	return fmt.Sprintf("beexar: gateway returned %d api_code=%s: %s", e.HTTPStatus, e.APICode, e.APIMessage)
}

// Retryable reports whether the call is worth repeating. A 400 on this contract
// is terminal — retrying it changes nothing.
func (e *APIError) Retryable() bool { return e.HTTPStatus >= 500 }

// SignatureError reports whether the gateway rejected the request signature.
// Keyed on api_code, because the status alone is ambiguous.
func (e *APIError) SignatureError() bool { return e.APICode == APICodeForbidden }

func newAPIError(status int, body []byte) *APIError {
	var env ErrorEnvelope
	_ = json.Unmarshal(body, &env)
	msg := env.Meta.APIMessage
	if msg == "" {
		msg = env.Msg
	}
	if msg == "" {
		msg = http.StatusText(status)
	}
	code := env.Meta.APICode
	if code == "" {
		code = fmt.Sprint(status)
	}
	twirp := env.Code
	if twirp == "" {
		twirp = TwirpCodeInternal
	}
	return &APIError{HTTPStatus: status, Code: twirp, APICode: code, APIMessage: msg}
}
