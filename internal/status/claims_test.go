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
	"testing"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
)

func claims(n int, nameLen int) []releasesv1alpha1.InventoryEntry {
	out := make([]releasesv1alpha1.InventoryEntry, 0, n)
	for i := range n {
		name := fmt.Sprintf("claim-%02d", i)
		if nameLen > len(name) {
			name += strings.Repeat("x", nameLen-len(name))
		}
		out = append(out, releasesv1alpha1.InventoryEntry{Kind: "PersistentVolumeClaim", Version: "v1", Namespace: "media", Name: name})
	}
	return out
}

func TestClaimsKeptNote(t *testing.T) {
	t.Run("one claim is named with its count", func(t *testing.T) {
		note := ClaimsKeptNote(claims(1, 0))
		if !strings.HasPrefix(note, "Kept 1 PersistentVolumeClaim(s) and the data on them: media/claim-00. ") {
			t.Fatalf("unexpected note: %s", note)
		}
		for _, want := range []string{"no longer tracked", "kubectl delete pvc", "from now on, set spec.dataPolicy to Delete"} {
			if !strings.Contains(note, want) {
				t.Errorf("note lacks %q: %s", want, note)
			}
		}
		if strings.Contains(note, "more") {
			t.Errorf("note of one claim must not count a rest: %s", note)
		}
	})

	t.Run("ten claims are all named", func(t *testing.T) {
		note := ClaimsKeptNote(claims(10, 0))
		if !strings.Contains(note, "media/claim-09.") || strings.Contains(note, "more") {
			t.Fatalf("unexpected note: %s", note)
		}
	})

	t.Run("twelve claims name ten and count two", func(t *testing.T) {
		note := ClaimsKeptNote(claims(12, 0))
		if !strings.Contains(note, "Kept 12 ") || !strings.Contains(note, "media/claim-09 and 2 more.") {
			t.Fatalf("unexpected note: %s", note)
		}
		if strings.Contains(note, "claim-10") {
			t.Fatalf("the eleventh claim must not be named: %s", note)
		}
	})

	t.Run("long names never take the note past the event limit", func(t *testing.T) {
		kept := claims(10, 253)
		note := ClaimsKeptNote(kept)
		if len(note) > eventNoteLimit {
			t.Fatalf("note is %d characters, limit %d", len(note), eventNoteLimit)
		}
		if !strings.Contains(note, kept[0].Name) || !strings.Contains(note, " more.") {
			t.Fatalf("want the first claim named and the rest counted: %s", note)
		}
	})

	t.Run("a name too long to list is counted", func(t *testing.T) {
		note := ClaimsKeptNote(claims(1, 1100))
		if len(note) > eventNoteLimit || !strings.Contains(note, "1 claims, names too long to list.") {
			t.Fatalf("unexpected note (%d characters): %s", len(note), note)
		}
	})
}
