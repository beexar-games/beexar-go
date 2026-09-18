package beexar_test

import (
	"os"
	"path/filepath"
	"testing"
)

// conformanceRoot locates the shared fixtures.
//
// Two layouts, because this package is developed in the Beexar monorepo and
// published as a standalone repository. In the published repository the
// fixtures sit at the root as conformance/; in the monorepo they are the single
// copy under api/, shared with the Node, PHP and Python SDKs. The tests that
// ship to operators therefore run unchanged in both places.
func conformanceRoot(t *testing.T) string {
	t.Helper()
	for _, candidate := range []string{
		"conformance", // published repository
		"../../api/providers/softswiss/conformance", // monorepo
	} {
		if _, err := os.Stat(filepath.Join(candidate, "manifest.json")); err == nil {
			return candidate
		}
	}
	t.Fatal("conformance fixtures not found — looked in ./conformance and ../../api/providers/softswiss/conformance")
	return ""
}
