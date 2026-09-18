package beexar_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	beexar "github.com/beexar-games/beexar-go"
	"github.com/beexar-games/beexar-go/examples/inmemory"
)

// The cross-language conformance suite. The same fixtures are run by the Node,
// PHP and Python SDKs, so behaviour cannot drift between languages while there
// is only one set of cases.

type manifest struct {
	FixtureVersion int `json:"fixture_version"`
	Cases          []struct {
		Dir      string `json:"dir"`
		Endpoint string `json:"endpoint"`
		Kind     string `json:"kind"`
	} `json:"cases"`
}

type requestMeta struct {
	Endpoint string            `json:"endpoint"`
	Kind     string            `json:"kind"`
	Headers  map[string]string `json:"headers"`
}

type expectedResponse struct {
	Status int             `json:"status"`
	Body   json.RawMessage `json:"body"`
}

func readFile(t *testing.T, parts ...string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(append([]string{conformanceRoot(t)}, parts...)...))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return b
}

func readJSON(t *testing.T, into any, parts ...string) {
	t.Helper()
	if err := json.Unmarshal(readFile(t, parts...), into); err != nil {
		t.Fatalf("decode fixture %v: %v", parts, err)
	}
}

// explodingWallet makes the "boom" account fail with something that is NOT a
// WalletError, exercising the "handler returned the unexpected" path without
// putting a test hook in the reference wallet itself.
type explodingWallet struct{ *inmemory.Wallet }

func (e explodingWallet) Balance(ctx context.Context, req beexar.BalanceRequest, rc beexar.RequestContext) (beexar.BalanceResult, error) {
	if req.AccountID == "boom" {
		return beexar.BalanceResult{}, errSimulatedOutage
	}
	return e.Wallet.Balance(ctx, req, rc)
}

var errSimulatedOutage = &simulatedOutage{}

type simulatedOutage struct{}

func (*simulatedOutage) Error() string { return "simulated ledger outage" }

func TestWalletConformance(t *testing.T) {
	var m manifest
	readJSON(t, &m, "manifest.json")
	if len(m.Cases) < 20 {
		t.Fatalf("expected the full fixture set, got %d cases", len(m.Cases))
	}
	secret := string(readFile(t, "secret.txt"))

	for _, c := range m.Cases {
		t.Run(c.Dir, func(t *testing.T) {
			body := readFile(t, "cases", c.Dir, "request.body")

			var meta requestMeta
			readJSON(t, &meta, "cases", c.Dir, "request.json")

			var before, after inmemory.LedgerState
			readJSON(t, &before, "cases", c.Dir, "ledger.before.json")
			readJSON(t, &after, "cases", c.Dir, "ledger.after.json")

			var want expectedResponse
			readJSON(t, &want, "cases", c.Dir, "response.json")

			// A fixture whose stored signature does not match its own bytes
			// would quietly test nothing, so the suite re-derives it.
			signature := meta.Headers["X-REQUEST-SIGN"]
			if signature != "" && signature != "0000000000000000000000000000000000000000000000000000000000000000" {
				if got := beexar.Sign(body, secret); got != signature {
					t.Fatalf("fixture signature does not match its own body:\n stored %s\n derived %s", signature, got)
				}
			}

			wallet := inmemory.New(before)
			server, err := beexar.NewWalletServer(explodingWallet{wallet}, secret)
			if err != nil {
				t.Fatalf("new server: %v", err)
			}

			out := server.Dispatch(context.Background(), beexar.Route(meta.Endpoint), body, signature, nil)

			if out.Status != want.Status {
				t.Errorf("status: got %d, want %d (body %s)", out.Status, want.Status, out.Body)
			}
			if !jsonEqual(t, out.Body, want.Body) {
				t.Errorf("body:\n got  %s\n want %s", out.Body, want.Body)
			}
			if got := wallet.Snapshot(); !ledgerEqual(t, got, after) {
				gotJSON, _ := json.Marshal(got)
				wantJSON, _ := json.Marshal(after)
				t.Errorf("ledger:\n got  %s\n want %s", gotJSON, wantJSON)
			}
		})
	}
}

func jsonEqual(t *testing.T, a, b []byte) bool {
	t.Helper()
	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		return false
	}
	return reflect.DeepEqual(av, bv)
}

// ledgerEqual compares through JSON so map ordering and the tombstone slice
// order cannot make an equal ledger look different.
func ledgerEqual(t *testing.T, got, want inmemory.LedgerState) bool {
	t.Helper()
	normalise := func(s inmemory.LedgerState) map[string]any {
		if s.Accounts == nil {
			s.Accounts = map[string]inmemory.StoredAccount{}
		}
		if s.Transactions == nil {
			s.Transactions = map[string]inmemory.StoredTransaction{}
		}
		tomb := map[string]bool{}
		for _, id := range s.Tombstones {
			tomb[id] = true
		}
		raw, _ := json.Marshal(struct {
			Accounts     map[string]inmemory.StoredAccount     `json:"accounts"`
			Transactions map[string]inmemory.StoredTransaction `json:"transactions"`
			Tombstones   map[string]bool                       `json:"tombstones"`
		}{s.Accounts, s.Transactions, tomb})
		out := map[string]any{}
		_ = json.Unmarshal(raw, &out)
		return out
	}
	return reflect.DeepEqual(normalise(got), normalise(want))
}
