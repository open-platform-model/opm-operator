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

package status

import (
	"fmt"
	"strings"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
)

const (
	// claimsKeptMaxNames is the most claims a ClaimsKept note names.
	claimsKeptMaxNames = 10
	// eventNoteLimit is the longest note events.k8s.io/v1 accepts.
	eventNoteLimit = 1024
	// claimsKeptAdvice closes every ClaimsKept note.
	claimsKeptAdvice = " They are no longer tracked. Delete one with: kubectl delete pvc <name> -n <namespace>. " +
		"To let the operator delete claims from now on, set spec.dataPolicy to Delete."
)

// ClaimsKeptNote is the note of a ClaimsKept event: how many
// PersistentVolumeClaims were kept, which ones, and what to do about them.
// It names at most ten claims as <namespace>/<name>, and fewer when long
// names would take the note past the event note limit; the rest is a count.
func ClaimsKeptNote(kept []releasesv1alpha1.InventoryEntry) string {
	head := fmt.Sprintf("Kept %d PersistentVolumeClaim(s) and the data on them: ", len(kept))
	// Room for the names: the limit less the fixed text and the longest
	// possible "and N more" tail.
	budget := eventNoteLimit - len(head) - len(claimsKeptAdvice) - len(fmt.Sprintf(" and %d more.", len(kept)))

	var names []string
	used := 0
	for _, entry := range kept {
		if len(names) == claimsKeptMaxNames {
			break
		}
		name := entry.Namespace + "/" + entry.Name
		cost := len(name)
		if len(names) > 0 {
			cost += len(", ")
		}
		if used+cost > budget {
			break
		}
		names = append(names, name)
		used += cost
	}

	var b strings.Builder
	b.WriteString(head)
	b.WriteString(strings.Join(names, ", "))
	switch rest := len(kept) - len(names); {
	case rest == 0:
		b.WriteString(".")
	case len(names) == 0:
		fmt.Fprintf(&b, "%d claims, names too long to list.", rest)
	default:
		fmt.Fprintf(&b, " and %d more.", rest)
	}
	b.WriteString(claimsKeptAdvice)
	return b.String()
}
