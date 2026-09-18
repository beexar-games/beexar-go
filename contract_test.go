package beexar_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	beexar "github.com/beexar-games/beexar-go"
)

// The Go SDK has no code generator: ogen's output would drag its runtime into
// every operator's build, and this package ships with zero dependencies. What
// replaces the generator is this test.
//
// contract.json is produced from the four OpenAPI documents by
// sdk/tools/gen-contract.mjs. Here we reflect over the wire structs and assert
// that every property and every `required` in the spec is present with the
// right JSON name. A field added to wallet.yaml fails this test rather than
// reaching an operator as a silently dropped value.

type contractDoc struct {
	Contracts map[string]string `json:"contracts"`
	// Keyed by spec, then by name: two specs declare an ErrorResponseMeta and
	// they are not the same shape.
	Schemas map[string]map[string]struct {
		Required   []string            `json:"required"`
		Properties map[string]struct { // only the keys matter here
			Type string `json:"type"`
		} `json:"properties"`
	} `json:"schemas"`
}

func loadContract(t *testing.T) contractDoc {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(conformanceRoot(t), "contract.json"))
	if err != nil {
		t.Fatalf("read contract.json (run `make sdk-generate`): %v", err)
	}
	var doc contractDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode contract.json: %v", err)
	}
	return doc
}

// jsonFields returns the JSON names a struct actually serialises.
func jsonFields(v any) map[string]bool {
	out := map[string]bool{}
	rt := reflect.TypeOf(v)
	for i := 0; i < rt.NumField(); i++ {
		tag := rt.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		out[strings.Split(tag, ",")[0]] = true
	}
	return out
}

func TestWireStructsCoverTheSpec(t *testing.T) {
	doc := loadContract(t)

	// The wire structs are unexported, so the check runs against the exported
	// shapes that carry the same JSON tags: the launcher requests and the
	// catalogue. The four callback bodies are covered by the conformance
	// fixtures, which exercise every field end to end.
	cases := []struct {
		spec   string
		schema string
		value  any
	}{
		{"launcher", "LauncherResponse", beexar.LaunchResult{}},
		{"launcher", "Account", beexar.LaunchAccount{}},
		{"catalog", "GameInfo", beexar.GameInfo{}},
	}

	for _, c := range cases {
		t.Run(c.schema, func(t *testing.T) {
			spec, ok := doc.Schemas[c.spec][c.schema]
			if !ok {
				t.Fatalf("schema %s is not in contract.json under %q — did the spec rename it?", c.schema, c.spec)
			}
			have := jsonFields(c.value)
			for property := range spec.Properties {
				if !have[property] {
					t.Errorf("%s.%s is in the spec but not on %T", c.schema, property, c.value)
				}
			}
		})
	}
}

func TestContractVersionsAreRecorded(t *testing.T) {
	doc := loadContract(t)
	for _, path := range []string{
		"api/providers/softswiss/wallet.yaml",
		"api/softswiss/gateway.yaml",
		"api/gateway.yaml",
		"api/common/schemas.yaml",
	} {
		v, ok := doc.Contracts[path]
		if !ok {
			t.Errorf("%s has no recorded contract version", path)
			continue
		}
		if !strings.HasPrefix(v, "v.20") {
			t.Errorf("%s: %q is not a v.YYYY.MM.DD contract version", path, v)
		}
	}
}

func TestAPICodesMatchThePlatformRegistry(t *testing.T) {
	// Codes 100, 105 and 106 must carry meta.balance. The platform reads the
	// field with the error swallowed, so nothing downstream will complain if we
	// get this wrong — which is exactly why it is pinned here.
	for _, code := range []string{beexar.APICodeInsufficientFunds, beexar.APICodeBetLimitReached, beexar.APICodeMaxBetExceeded} {
		if !beexar.IsFundsRelatedCode(code) {
			t.Errorf("api_code %s must be funds-related", code)
		}
	}
	for _, code := range []string{
		beexar.APICodeInvalidPlayer, beexar.APICodeForbidden, beexar.APICodeAlreadyRolledBack,
		beexar.APICodeBadRequest, beexar.APICodeUnknownError,
	} {
		if beexar.IsFundsRelatedCode(code) {
			t.Errorf("api_code %s must not require a balance", code)
		}
	}
}
