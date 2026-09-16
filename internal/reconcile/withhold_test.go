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

package reconcile

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/open-platform-model/opm-operator/internal/shrink"
)

// refuseByName is a ClaimShrinkDecider that refuses the named resources,
// standing in for the real comparison against the stored claim.
type refuseByName struct {
	names []string
	err   error
}

func (d refuseByName) Decide(_ context.Context, rendered *unstructured.Unstructured) (shrink.Decision, error) {
	if d.err != nil {
		return shrink.Decision{}, d.err
	}
	if !slices.Contains(d.names, rendered.GetName()) {
		return shrink.Decision{}, nil
	}
	return shrink.Decision{
		Claim:      rendered.GetName(),
		Dropped:    []string{"opmodel.dev/contract.Backup"},
		Dependents: []string{"team-b/needs-backup"},
	}, nil
}

func named(names ...string) []*unstructured.Unstructured {
	out := make([]*unstructured.Unstructured, 0, len(names))
	for _, name := range names {
		u := &unstructured.Unstructured{}
		u.SetName(name)
		out = append(out, u)
	}
	return out
}

func namesOf(resources []*unstructured.Unstructured) []string {
	out := make([]string, 0, len(resources))
	for _, r := range resources {
		out = append(out, r.GetName())
	}
	return out
}

func TestWithholdRefused(t *testing.T) {
	tests := []struct {
		name        string
		rendered    []string
		refuse      []string
		wantApplied []string
		wantRefused int
	}{
		{
			// The common reconcile: nothing is judged refusable.
			name:        "nothing refused leaves the list alone",
			rendered:    []string{"a", "b", "c"},
			wantApplied: []string{"a", "b", "c"},
		},
		{
			// Order matters: the surviving resources keep theirs.
			name:        "a refusal in the middle drops only that one",
			rendered:    []string{"a", "b", "c"},
			refuse:      []string{"b"},
			wantApplied: []string{"a", "c"},
			wantRefused: 1,
		},
		{
			name:        "a refusal at the head drops only that one",
			rendered:    []string{"a", "b", "c"},
			refuse:      []string{"a"},
			wantApplied: []string{"b", "c"},
			wantRefused: 1,
		},
		{
			name:        "a refusal at the tail drops only that one",
			rendered:    []string{"a", "b", "c"},
			refuse:      []string{"c"},
			wantApplied: []string{"a", "b"},
			wantRefused: 1,
		},
		{
			// An instance renders at most one claim today, but the filter
			// must not depend on that.
			name:        "several refusals drop each of them",
			rendered:    []string{"a", "b", "c", "d"},
			refuse:      []string{"b", "d"},
			wantApplied: []string{"a", "c"},
			wantRefused: 2,
		},
		{
			// Refusing everything must leave an empty apply list, not the
			// original one.
			name:        "refusing every resource empties the apply list",
			rendered:    []string{"a", "b"},
			refuse:      []string{"a", "b"},
			wantApplied: []string{},
			wantRefused: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			applyList, refused, err := withholdRefused(
				context.Background(),
				refuseByName{names: tt.refuse},
				named(tt.rendered...),
			)
			if err != nil {
				t.Fatalf("withholdRefused: %v", err)
			}
			if got := namesOf(applyList); !slices.Equal(got, tt.wantApplied) {
				t.Errorf("apply list = %v, want %v", got, tt.wantApplied)
			}
			if len(refused) != tt.wantRefused {
				t.Errorf("refused = %d decisions, want %d", len(refused), tt.wantRefused)
			}
		})
	}
}

// A verdict that could not be reached is an error, never a silent allow:
// applying on an unreadable answer is the abandonment the refusal prevents.
func TestWithholdRefused_PropagatesDeciderError(t *testing.T) {
	wantErr := errors.New("reading stored claim: connection refused")

	applyList, refused, err := withholdRefused(
		context.Background(),
		refuseByName{err: wantErr},
		named("a", "b"),
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
	if applyList != nil || refused != nil {
		t.Errorf("a failed verdict returned a list: apply=%v refused=%v", applyList, refused)
	}
}

// The message is what an operator acts on, so it must carry all three facts:
// the claim, the contracts it would stop providing, and how many instances
// still demand them.
func TestRefusalMessage(t *testing.T) {
	msg := refusalMessage([]shrink.Decision{{
		Claim:      "team-a.provider",
		Dropped:    []string{"opmodel.dev/contract.Backup"},
		Dependents: []string{"team-b/needs-backup", "team-c/also-needs-backup"},
	}})

	for _, want := range []string{
		"team-a.provider",
		"opmodel.dev/contract.Backup",
		"2 module instance(s)",
		"team-b/needs-backup",
		"team-c/also-needs-backup",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message does not carry %q: %s", want, msg)
		}
	}
}
