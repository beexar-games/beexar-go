package beexar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// MaxBodyBytes mirrors the gateway's own cap on an operator request body, so a
// body it would never have sent cannot make your process allocate.
const MaxBodyBytes = 64 * 1024

// WalletServerOption configures a WalletServer.
type WalletServerOption func(*WalletServer)

// WithMaxBodyBytes overrides the request body cap.
func WithMaxBodyBytes(n int) WalletServerOption {
	return func(s *WalletServer) { s.maxBodyBytes = n }
}

// WithWarning sets the sink for diagnostics explaining WHY a signature failed.
// The default writes nothing; pass a logger to see them.
func WithWarning(fn func(message string)) WalletServerOption {
	return func(s *WalletServer) { s.warn = fn }
}

// WalletServer turns the four callbacks into one pure function.
//
// Dispatch takes bytes and a signature and returns a status and a body. No
// framework type crosses that boundary and it never sees a parsed body from the
// outside, so signing the wrong thing is not expressible.
type WalletServer struct {
	handler      WalletHandler
	apiSecret    string
	maxBodyBytes int
	warn         func(string)
}

// NewWalletServer builds a server. It fails on an empty secret: the gateway
// treats an empty secret as an automatic verification failure, so a server
// built with one would authenticate nothing while looking healthy.
func NewWalletServer(handler WalletHandler, apiSecret string, opts ...WalletServerOption) (*WalletServer, error) {
	if handler == nil {
		return nil, errors.New("beexar: handler is required")
	}
	if strings.TrimSpace(apiSecret) == "" {
		return nil, errors.New("beexar: apiSecret is required and must be non-empty")
	}
	s := &WalletServer{
		handler:      handler,
		apiSecret:    apiSecret,
		maxBodyBytes: MaxBodyBytes,
		warn:         func(string) {},
	}
	for _, o := range opts {
		o(s)
	}
	return s, nil
}

// DispatchResponse is what the transport should write back.
type DispatchResponse struct {
	Status int
	Body   []byte
}

// Dispatch verifies, parses, validates and routes one callback.
func (s *WalletServer) Dispatch(ctx context.Context, route Route, rawBody []byte, signature string, headers map[string]string) DispatchResponse {
	res, err := s.dispatch(ctx, route, rawBody, signature, headers)
	if err == nil {
		return res
	}

	var we *WalletError
	if errors.As(err, &we) {
		return encodeError(we)
	}
	// Anything the handler returned that is not a WalletError: answer 500 and
	// say nothing about it. The platform retries 5xx with the same id_provider,
	// so this is the retryable shape, and the message never leaves the process.
	return encodeError(ErrInternal(""))
}

func (s *WalletServer) dispatch(ctx context.Context, route Route, rawBody []byte, signature string, headers map[string]string) (DispatchResponse, error) {
	if !validRoute(route) {
		return DispatchResponse{}, ErrNotFound(fmt.Sprintf("unknown route: %s", route))
	}
	if len(rawBody) > s.maxBodyBytes {
		return DispatchResponse{}, ErrBadRequest("request body too large")
	}
	if !Verify(rawBody, signature, s.apiSecret) {
		s.explainSignatureFailure(rawBody, signature)
		return DispatchResponse{}, ErrInvalidSignature()
	}

	rc := RequestContext{Route: route, RawBody: rawBody, Headers: headers}

	switch route {
	case RouteBalance:
		var w wireBalanceRequest
		if err := decode(rawBody, &w); err != nil {
			return DispatchResponse{}, err
		}
		req, err := w.toRequest()
		if err != nil {
			return DispatchResponse{}, err
		}
		out, err := s.handler.Balance(ctx, req, rc)
		if err != nil {
			return DispatchResponse{}, err
		}
		return encodeJSON(wireBalanceResponse{Balance: out.Balance.String()})

	case RouteBetWin:
		var w wireBetWinRequest
		if err := decode(rawBody, &w); err != nil {
			return DispatchResponse{}, err
		}
		req, err := w.toRequest()
		if err != nil {
			return DispatchResponse{}, err
		}
		out, err := s.handler.BetWin(ctx, req, rc)
		if err != nil {
			return DispatchResponse{}, err
		}
		txs := make([]wireBetWinResponseTransaction, 0, len(out.Transactions))
		for _, t := range out.Transactions {
			bonus := ZeroMoney
			if t.BonusAmount != nil {
				bonus = *t.BonusAmount
			}
			txs = append(txs, wireBetWinResponseTransaction{
				IDProvider:  t.IDProvider,
				ID:          t.ID,
				BonusAmount: bonus.FormatBonus(),
			})
		}
		return encodeJSON(wireBetWinResponse{RoundID: out.RoundID, Transactions: txs, Balance: out.Balance.String()})

	case RouteRollback:
		var w wireRollbackRequest
		if err := decode(rawBody, &w); err != nil {
			return DispatchResponse{}, err
		}
		req, err := w.toRequest()
		if err != nil {
			return DispatchResponse{}, err
		}
		out, err := s.handler.Rollback(ctx, req, rc)
		if err != nil {
			return DispatchResponse{}, err
		}
		txs := make([]wireRollbackResponseTransaction, 0, len(out.Transactions))
		for _, t := range out.Transactions {
			txs = append(txs, wireRollbackResponseTransaction{IDProvider: t.IDProvider, ID: t.ID})
		}
		return encodeJSON(wireRollbackResponse{Balance: out.Balance.String(), RoundID: out.RoundID, Transactions: txs})

	case RouteFinish:
		var w wireFinishRequest
		if err := decode(rawBody, &w); err != nil {
			return DispatchResponse{}, err
		}
		req, err := w.toRequest()
		if err != nil {
			return DispatchResponse{}, err
		}
		out, err := s.handler.Finish(ctx, req, rc)
		if err != nil {
			return DispatchResponse{}, err
		}
		return encodeJSON(wireBalanceResponse{Balance: out.Balance.String()})
	}

	return DispatchResponse{}, ErrNotFound("unknown route")
}

// explainSignatureFailure says why a signature failed when the SDK can tell.
//
// Nearly every failed integration is the same bug: a body-parsing middleware
// consumed the stream, and what reaches the SDK is a re-serialisation rather
// than the bytes that were signed. That is unrecoverable, but it is
// recognisable, and naming it saves days.
func (s *WalletServer) explainSignatureFailure(rawBody []byte, signature string) {
	switch {
	case signature == "":
		s.warn("X-REQUEST-SIGN header is missing from the request")
	case len(rawBody) == 0:
		s.warn("the request body reaching the SDK is empty, which usually means something " +
			"already consumed the stream — read the body once and hand the same bytes to Dispatch")
	default:
		var parsed any
		if err := json.Unmarshal(rawBody, &parsed); err == nil {
			if reserialised, err := json.Marshal(parsed); err == nil && string(reserialised) == string(rawBody) {
				s.warn("the body reaching the SDK is byte-identical to a JSON re-serialisation. " +
					"If something parsed the body before the SDK saw it, the bytes that were " +
					"signed are already lost and the signature can never match.")
				return
			}
		}
		s.warn("signature mismatch: check that the API secret matches the one in the backoffice")
	}
}

// NewHandler wires the four callbacks into an http.Handler.
//
// It matches on the path SUFFIX because the mount path belongs to you: the
// gateway POSTs to the full callback URL configured in the backoffice, prefix
// and all, so /api/v2/psp/betwin is as valid as /betwin.
func (s *WalletServer) NewHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route, ok := RouteFromPath(r.URL.Path)
		if !ok || r.Method != http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"code":"invalid_argument","msg":"not found","meta":{"api_code":"404","api_message":"not found"}}`))
			return
		}

		// Read at most one byte past the cap: enough to know the body is too
		// large, without paying for the rest of what a sender claims to have.
		body, err := io.ReadAll(io.LimitReader(r.Body, int64(s.maxBodyBytes)+1))
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code":"invalid_argument","msg":"could not read request body","meta":{"api_code":"400","api_message":"could not read request body"}}`))
			return
		}

		headers := make(map[string]string, len(r.Header))
		for k, v := range r.Header {
			if len(v) > 0 {
				headers[strings.ToLower(k)] = v[0]
			}
		}

		out := s.Dispatch(r.Context(), route, body, r.Header.Get(SignatureHeader), headers)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(out.Status)
		_, _ = w.Write(out.Body)
	})
}

// RouteFromPath maps a request path to one of the four callback routes,
// matching on the suffix so any mount prefix works.
func RouteFromPath(path string) (Route, bool) {
	clean := strings.TrimRight(path, "/")
	if clean == "" {
		clean = "/"
	}
	for _, r := range Routes {
		if clean == string(r) || strings.HasSuffix(clean, string(r)) {
			return r, true
		}
	}
	return "", false
}

func validRoute(r Route) bool {
	for _, known := range Routes {
		if r == known {
			return true
		}
	}
	return false
}

func decode(body []byte, into any) error {
	if err := json.Unmarshal(body, into); err != nil {
		return ErrBadRequest("malformed JSON body")
	}
	return nil
}

func encodeJSON(v any) (DispatchResponse, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return DispatchResponse{}, ErrInternal("")
	}
	return DispatchResponse{Status: http.StatusOK, Body: b}, nil
}

func encodeError(we *WalletError) DispatchResponse {
	b, err := json.Marshal(we.Envelope())
	if err != nil {
		return DispatchResponse{Status: http.StatusInternalServerError, Body: []byte(`{"code":"internal","msg":"internal error","meta":{"api_code":"500","api_message":"internal error"}}`)}
	}
	return DispatchResponse{Status: we.Status, Body: b}
}
