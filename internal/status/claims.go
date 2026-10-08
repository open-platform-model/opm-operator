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
	// claimRefusalLimit is the most of the API server's refusal a
	// ClaimConflict note repeats. The refusal of a claim update carries a
	// diff of the whole spec after its first line.
	claimRefusalLimit = 300
	// claimConflictAdvice closes every ClaimConflict note.
	claimConflictAdvice = " The claim and its data are kept. Revert the change, or move the data and delete " +
		"the claim yourself, or set spec.dataPolicy to Delete to let the operator delete and recreate the claim."
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

// ClaimConflictNote is the message of the ClaimConflict reason, on the Ready
// condition and on the event: which PersistentVolumeClaim an apply left as it
// is where a forced recreate would have deleted it, which fields the API
// server refused to update and why, and the ways out. fields and refusal are
// what the API server reported; both are empty when the claim was kept
// without a refusal at hand. Of the refusal the note keeps the first line,
// cut to a fixed length.
func ClaimConflictNote(namespace, name string, fields []string, refusal string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "PersistentVolumeClaim %s/%s: ", namespace, name)
	if refusal == "" {
		b.WriteString("the update needs the claim deleted and created again.")
		b.WriteString(claimConflictAdvice)
		return b.String()
	}

	b.WriteString("the API server refused the update")
	if len(fields) > 0 {
		b.WriteString(" of " + strings.Join(fields, ", "))
	}
	line, _, _ := strings.Cut(strings.TrimSpace(refusal), "\n")
	if len(line) > claimRefusalLimit {
		line = line[:claimRefusalLimit] + "..."
	}
	fmt.Fprintf(&b, " (%s). Nothing was applied.", line)
	b.WriteString(claimConflictAdvice)
	return b.String()
}
