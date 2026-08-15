package reconcile

import (
	"errors"
	"fmt"
	"testing"

	"k8s.io/client-go/tools/events"

	"github.com/open-platform-model/library/opm/compile"
	oerrors "github.com/open-platform-model/library/opm/errors"

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

// unresolvedDemandsJoin mirrors the library's compile gate
// (compile/module.go): a bare errors.Join carrying *UnresolvedDemandsError
// alongside *UnmatchedComponentsError, wrapped once by the renderer. If the
// library changes this shape, these tests fail rather than letting typed
// routing silently degrade to the string fallback.
func unresolvedDemandsJoin() error {
	return errors.Join(
		&oerrors.UnresolvedDemandsError{Demands: []oerrors.UnresolvedDemand{
			{Component: "web", FQN: "opmodel.dev/contracts/volume", Kind: "resource"},
		}},
		&compile.UnmatchedComponentsError{Components: []string{"web"}},
	)
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
			name:        "unresolved demands joined and wrapped by renderer",
			err:         fmt.Errorf("compiling module release: %w", unresolvedDemandsJoin()),
			wantOutcome: FailedStalled,
			wantReason:  status.ResolutionFailedReason,
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
			name:        "transform-failure join stays RenderFailed",
			err:         fmt.Errorf("compiling module release: %w", fmt.Errorf("executing transforms: %w", errors.Join(errors.New("boom")))),
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
		})
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
			name: "unresolved demands joined and wrapped by renderer",
			err:  fmt.Errorf("compiling module instance: %w", unresolvedDemandsJoin()),
			want: status.ResolutionFailedReason,
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
