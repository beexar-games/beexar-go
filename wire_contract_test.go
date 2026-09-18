package beexar

// This test lives INSIDE the package on purpose: the wire structs are
// unexported, and they are the ones that have to match the contract field for
// field. contract_test.go covers the exported launcher and catalogue types;
// without this file the four wallet callbacks — the half that moves money —
// would be checked only by whatever the conformance fixtures happen to
// exercise, so a property added to wallet.yaml and used by nobody yet would be
// dropped silently.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

type contractSchema struct {
	Required   []string                   `json:"required"`
	Properties map[string]json.RawMessage `json:"properties"`
}

// Schemas are keyed by spec, then by name — two specs declare an
// ErrorResponseMeta and they are not the same shape.
type contractFile struct {
	Schemas map[string]map[string]contractSchema `json:"schemas"`
}

func loadContractSchemas(t *testing.T, spec string) map[string]contractSchema {
	t.Helper()
	for _, candidate := range []string{
		"conformance/contract.json",
		"../../api/providers/softswiss/conformance/contract.json",
	} {
		raw, err := os.ReadFile(filepath.Clean(candidate))
		if err != nil {
			continue
		}
		var doc contractFile
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("decode %s: %v", candidate, err)
		}
		schemas, ok := doc.Schemas[spec]
		if !ok {
			t.Fatalf("contract.json has no schemas for spec %q", spec)
		}
		return schemas
	}
	t.Fatal("contract.json not found — run `make sdk-generate`")
	return nil
}

func jsonTagNames(v any) map[string]bool {
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

// TestWireStructsMatchTheContract asserts, for every wallet schema, that each
// property the spec declares has a field on the struct that carries it — and
// that the struct invents nothing the spec does not have.
func TestWireStructsMatchTheContract(t *testing.T) {
	schemas := loadContractSchemas(t, "wallet")

	cases := []struct {
		schema string
		value  any
	}{
		{"PlayerBalanceRequest", wireBalanceRequest{}},
		{"PlayerBalanceResponse", wireBalanceResponse{}},
		{"RoundBetWinRequest", wireBetWinRequest{}},
		{"RoundBetWinRequestTransaction", wireBetWinTransaction{}},
		{"RoundBetWinResponse", wireBetWinResponse{}},
		{"RoundBetWinResponseTransaction", wireBetWinResponseTransaction{}},
		{"RoundRollbackRequest", wireRollbackRequest{}},
		{"RoundRollbackRequestTransaction", wireRollbackTransaction{}},
		{"RoundRollbackResponse", wireRollbackResponse{}},
		{"RoundRollbackResponseTransaction", wireRollbackResponseTransaction{}},
		{"RoundFinishRequest", wireFinishRequest{}},
		{"RoundFinishResponse", wireBalanceResponse{}}, // same single `balance` field
		{"ErrorResponse", ErrorEnvelope{}},
		{"ErrorResponseMeta", ErrorMeta{}},
	}

	for _, c := range cases {
		t.Run(c.schema, func(t *testing.T) {
			schema, ok := schemas[c.schema]
			if !ok {
				t.Fatalf("schema %s is not in contract.json — was it renamed in the spec?", c.schema)
			}
			have := jsonTagNames(c.value)

			var missing []string
			for property := range schema.Properties {
				if !have[property] {
					missing = append(missing, property)
				}
			}
			sort.Strings(missing)
			if len(missing) > 0 {
				t.Errorf("%s: the spec declares %v, which %T does not carry", c.schema, missing, c.value)
			}

			var invented []string
			for field := range have {
				if _, ok := schema.Properties[field]; !ok {
					invented = append(invented, field)
				}
			}
			sort.Strings(invented)
			if len(invented) > 0 {
				t.Errorf("%s: %T carries %v, which the spec does not declare", c.schema, c.value, invented)
			}
		})
	}
}

// TestRequiredFieldsAreValidated pins the fields the parser insists on against
// the spec's `required` list, so a newly required property cannot stay optional
// here.
func TestRequiredFieldsAreValidated(t *testing.T) {
	schemas := loadContractSchemas(t, "wallet")

	// What wire_convert.go actually rejects a request for.
	enforced := map[string][]string{
		"PlayerBalanceRequest":            {"account_id", "currency", "game_id"},
		"RoundBetWinRequest":              {"account_id", "currency", "game_id", "round_id", "transactions"},
		"RoundBetWinRequestTransaction":   {"amount", "id_provider", "type"},
		"RoundRollbackRequestTransaction": {"id_provider", "original_id_provider", "type"},
		"RoundFinishRequest":              {"account_id", "currency", "round_id"},
	}

	for schema, fields := range enforced {
		t.Run(schema, func(t *testing.T) {
			spec := append([]string(nil), schemas[schema].Required...)
			sort.Strings(spec)
			want := append([]string(nil), fields...)
			sort.Strings(want)
			if !reflect.DeepEqual(spec, want) {
				t.Errorf("the parser enforces %v but the spec requires %v", want, spec)
			}
		})
	}

	// The one deliberate divergence, asserted so it stays deliberate: the spec
	// requires game_id on rollback and the platform always sends it, but the
	// SDK accepts its absence rather than refuse a request it could serve.
	found := false
	for _, r := range schemas["RoundRollbackRequest"].Required {
		if r == "game_id" {
			found = true
		}
	}
	if !found {
		t.Error("game_id is no longer required on RoundRollbackRequest — the liberal-parse comment in wire_convert.go is now wrong")
	}
}
