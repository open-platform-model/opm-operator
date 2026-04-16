/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import "testing"

// TODO: Test registry config precedence (--registry flag > OPM_REGISTRY env > CUE default).
//
// WHY this is deferred:
//
// The precedence logic is inline in main() after flag.Parse():
//
//	if registry == "" {
//	    registry = os.Getenv("OPM_REGISTRY")
//	}
//
// The `registry` variable is bound to --registry via flag.StringVar, so testing
// requires either process-level binary execution with different flag/env combos,
// or extracting the logic into a standalone function. Neither is warranted for
// 3 lines of trivially-correct code at this stage.
//
// HOW to implement when ready:
//
// Option A (preferred) — extract a testable function:
//
//	func resolveRegistry(flagValue string) string {
//	    if flagValue != "" { return flagValue }
//	    if env := os.Getenv("OPM_REGISTRY"); env != "" { return env }
//	    return ""
//	}
//
// Then table-driven tests covering:
//   - Flag set, env set      → flag wins
//   - Flag empty, env set    → env wins
//   - Flag empty, env empty  → empty (CUE default resolution)
//   - Flag set, env empty    → flag wins
//
// Option B — cover via e2e test that verifies controller startup logs show the
// expected registry value under different flag/env configurations.
func TestRegistryConfigPrecedence_TODO(t *testing.T) {
	t.Skip("Registry config precedence not yet unit-testable — logic is inline in main(). See TODO above.")
}
