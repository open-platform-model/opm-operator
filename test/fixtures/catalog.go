// Catalog subscription coordinate for registry-backed specs. Operator-local:
// not part of the byte-identical fixture flow shared with cli (fixtures.sh,
// fixtures.go, fixtures_test.go), so `task fixtures:lint` does not cover it.
package fixtures

import "os"

// CatalogVersion is the exact catalog build the registry-backed specs
// subscribe to (enhancement 0010 D14: a subscription names one published
// build; there is no range vocabulary). Overridable via
// OPM_TEST_CATALOG_VERSION so a fixture republish does not require a code
// edit; the default tracks the pin in config/samples. Single source for
// every suite — a catalog bump edits this default once, never a per-suite
// copy.
func CatalogVersion() string {
	if v := os.Getenv("OPM_TEST_CATALOG_VERSION"); v != "" {
		return v
	}
	return "4.0.1"
}
