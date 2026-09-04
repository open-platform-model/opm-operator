package reconcile

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"k8s.io/client-go/tools/events"

	oerrors "github.com/open-platform-model/library/opm/errors"
	"github.com/open-platform-model/library/opm/kernel"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	"github.com/open-platform-model/opm-operator/internal/render"
	"github.com/open-platform-model/opm-operator/internal/status"
)

// identityErr mirrors the acquire path: verifyModuleIdentity returns a bare
// value-typed oerrors.IdentityError, wrapped once by the module renderer.
func identityErr() error {
	return oerrors.IdentityError{
		Artifact:   "module",
		Field:      "path",
		Declared:   "opmodel.dev/modules/other",
		Fetched:    "opmodel.dev/modules/demo",
		Coordinate: "opmodel.dev/modules/demo v1.0.0",
	}
}

// renderRefused mirrors the kernel's fail-closed gate: a *kernel.RenderError
// carrying the diagnostics and the joined typed causes, wrapped once by the
// renderer. If the library changes this shape, these tests fail rather than
// letting typed routing silently degrade to the string fallback.
func renderRefused(causes ...error) error {
	return fmt.Errorf("rendering module instance: %w", &kernel.RenderError{Err: errors.Join(causes...)})
}

func unresolvedDemands() error {
	return &oerrors.UnresolvedDemandsError{Demands: []oerrors.UnresolvedDemand{
		{Component: "web", FQN: "opmodel.dev/contracts/volume", Kind: "resource"},
	}}
}

func unmatchedComponents() error {
	return &oerrors.UnmatchedComponentsError{Components: []string{"web"}}
}

func overSubscribed() error {
	return oerrors.OverSubscribedContractError{Key: "opmodel.dev/contracts/ingress", Catalogs: []string{"a@v1", "b@v1"}}
}

func transformFailure() error {
	return &oerrors.TransformError{ComponentName: "web", TransformerFQN: "deployment", Cause: errors.New("boom")}
}

// skewRefused mirrors the kernel's pre-evaluation refusal under SkewRefuse:
// a plain error joining one *oerrors.SkewError per skewed path, wrapped by
// the renderer.
func skewRefused() error {
	return fmt.Errorf("rendering module instance: %w", fmt.Errorf("render refused before evaluation: %w", errors.Join(
		&oerrors.SkewError{Path: "opmodel.dev/catalogs/opm@v4", ModuleVersion: "v4.1.0", PlatformVersion: "v4.0.1"},
	)))
}

func TestClassifyRenderError(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantOutcome Outcome
		wantReason  string
	}{
		{
			name:        "identity error bare",
			err:         identityErr(),
			wantOutcome: FailedStalled,
			wantReason:  status.ResolutionFailedReason,
		},
		{
			name:        "identity error wrapped by renderer",
			err:         fmt.Errorf("acquiring module: %w", identityErr()),
			wantOutcome: FailedStalled,
			wantReason:  status.ResolutionFailedReason,
		},
		{
			name:        "unresolved demands bare",
			err:         &oerrors.UnresolvedDemandsError{},
			wantOutcome: FailedStalled,
			wantReason:  status.ResolutionFailedReason,
		},
		{
			name:        "unresolved demands and unmatched components refused by the gate",
			err:         renderRefused(unresolvedDemands(), unmatchedComponents()),
			wantOutcome: FailedStalled,
			wantReason:  status.ResolutionFailedReason,
		},
		{
			name:        "unmatched components alone refused by the gate",
			err:         renderRefused(unmatchedComponents()),
			wantOutcome: FailedStalled,
			wantReason:  status.ResolutionFailedReason,
		},
		{
			name:        "skew refused under the Refuse policy",
			err:         skewRefused(),
			wantOutcome: FailedStalled,
			wantReason:  status.SkewRefusedReason,
		},
		{
			name:        "over-subscribed provider contract is a render failure",
			err:         renderRefused(overSubscribed()),
			wantOutcome: FailedStalled,
			wantReason:  status.RenderFailedReason,
		},
		{
			name:        "transform failure is a render failure",
			err:         renderRefused(transformFailure()),
			wantOutcome: FailedStalled,
			wantReason:  status.RenderFailedReason,
		},
		{
			name:        "platform not ready keeps its gate",
			err:         render.ErrPlatformNotReady,
			wantOutcome: FailedTransient,
			wantReason:  status.PlatformNotReadyReason,
		},
		{
			name:        "loader string fallback still routes",
			err:         errors.New("synthesizing release: no such module"),
			wantOutcome: FailedStalled,
			wantReason:  status.ResolutionFailedReason,
		},
		{
			name:        "plain evaluation error stays RenderFailed",
			err:         errors.New("field values.replicas: conflicting values"),
			wantOutcome: FailedStalled,
			wantReason:  status.RenderFailedReason,
		},
		{
			name:        "pre-evaluation operator defect stays RenderFailed",
			err:         fmt.Errorf("rendering module instance: %w", errors.New(`platform "cluster" carries no Source`)),
			wantOutcome: FailedStalled,
			wantReason:  status.RenderFailedReason,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mi := &releasesv1alpha1.ModuleInstance{}
			rec := events.NewFakeRecorder(2)
			outcome, msg := classifyRenderError(mi, rec, tt.err)
			if outcome != tt.wantOutcome {
				t.Errorf("outcome = %v, want %v", outcome, tt.wantOutcome)
			}
			if msg != tt.err.Error() {
				t.Errorf("message = %q, want %q", msg, tt.err.Error())
			}
			es := drainEvents(rec)
			if countEventsWithReason(es, tt.wantReason) != 1 {
				t.Errorf("events %v carry reason %q %d time(s), want 1", es, tt.wantReason, countEventsWithReason(es, tt.wantReason))
			}
			if len(es) != 1 || !strings.Contains(es[0], tt.err.Error()) {
				t.Errorf("events %v, want exactly one carrying the error message %q", es, tt.err.Error())
			}
		})
	}
}

func TestClassifyRenderError_SkewMessageNamesPathAndVersions(t *testing.T) {
	mi := &releasesv1alpha1.ModuleInstance{}
	_, msg := classifyRenderError(mi, events.NewFakeRecorder(2), skewRefused())
	for _, want := range []string{"opmodel.dev/catalogs/opm@v4", "v4.1.0", "v4.0.1"} {
		if !strings.Contains(msg, want) {
			t.Errorf("SkewRefused message %q must name %q", msg, want)
		}
	}
}

func TestRenderErrorReason(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "unsupported kind wins",
			err:  fmt.Errorf("%w: not a release", render.ErrUnsupportedKind),
			want: status.UnsupportedKindReason,
		},
		{
			name: "unresolved demands and unmatched components refused by the gate",
			err:  renderRefused(unresolvedDemands(), unmatchedComponents()),
			want: status.ResolutionFailedReason,
		},
		{
			name: "skew refused under the Refuse policy",
			err:  skewRefused(),
			want: status.SkewRefusedReason,
		},
		{
			name: "over-subscribed provider contract is a render failure",
			err:  renderRefused(overSubscribed()),
			want: status.RenderFailedReason,
		},
		{
			name: "loader string fallback still routes",
			err:  errors.New("loading package: open release.cue: no such file"),
			want: status.ResolutionFailedReason,
		},
		{
			name: "plain evaluation error stays RenderFailed",
			err:  errors.New("field values.replicas: conflicting values"),
			want: status.RenderFailedReason,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := renderErrorReason(tt.err); got != tt.want {
				t.Errorf("renderErrorReason() = %q, want %q", got, tt.want)
			}
		})
	}
}
