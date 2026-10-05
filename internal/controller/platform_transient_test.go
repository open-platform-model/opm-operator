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

package controller

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"testing"

	"cuelang.org/go/mod/modfile"
	"cuelang.org/go/mod/module"

	oerrors "github.com/open-platform-model/library/opm/errors"
	"github.com/open-platform-model/library/opm/helper/platformmodule"
)

// timeoutError is a net.Error whose Timeout() is true, modelling a dial/read
// deadline reached against the registry.
type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

var _ net.Error = timeoutError{}

// registryRefused is the *url.Error an unreachable registry returns.
func registryRefused() error {
	return &url.Error{Op: "Get", URL: "https://registry.invalid/v2/", Err: errors.New("connection refused")}
}

// The Platform rechecks quickly for the library's typed registry fetch
// failure (any kind) and an expired deadline, and slowly for everything
// else, including a raw network error the library did not classify.
func TestIsTransientFailure(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil is not transient",
			err:  nil,
			want: false,
		},
		{
			name: "typed fetch failure over a url.Error is transient",
			err: fmt.Errorf("resolving dependency testing.opmodel.dev/catalogs/example@v0.1.0: %w",
				&oerrors.FetchError{Kind: oerrors.FetchUnreachable, Err: registryRefused()}),
			want: true,
		},
		{
			name: "typed not-found fetch failure is transient",
			err: fmt.Errorf("resolving dependency testing.opmodel.dev/catalogs/does-not-exist@v9.9.9: %w",
				&oerrors.FetchError{Kind: oerrors.FetchNotFound, Err: errors.New("module not found")}),
			want: true,
		},
		{
			name: "context deadline exceeded is transient",
			err:  context.DeadlineExceeded,
			want: true,
		},
		{
			name: "raw url.Error the library did not classify is not transient",
			err:  registryRefused(),
			want: false,
		},
		{
			name: "raw timing-out net.Error is not transient",
			err:  timeoutError{},
			want: false,
		},
		{
			name: "module not found as text is not transient",
			err: fmt.Errorf("resolving dependency testing.opmodel.dev/catalogs/does-not-exist@v9.9.9: %w",
				errors.New("module not found")),
			want: false,
		},
		{
			name: "unclassifiable plain error is not transient",
			err:  errors.New("something went wrong"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTransientFailure(tt.err); got != tt.want {
				t.Errorf("isTransientFailure(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// urlErrorSource is a module-file source whose every fetch fails with a
// *url.Error, the shape of an unreachable registry.
type urlErrorSource struct{}

func (urlErrorSource) ModFile(context.Context, module.Version) (*modfile.File, error) {
	return nil, registryRefused()
}

// The Platform's closure walk classifies a registry failure itself
// (0021:D8:R12): the error failReconcile receives holds a *FetchError, so the
// Platform needs no network-error probe of its own and rechecks it quickly.
func TestIsTransientFailure_ClosureClassifiesFetchFailure(t *testing.T) {
	_, err := platformmodule.Closure(context.Background(), urlErrorSource{},
		[]platformmodule.Dep{{Path: "testing.opmodel.dev/catalogs/example@v0", Version: "v0.1.0"}})
	if err == nil {
		t.Fatal("Closure succeeded against a failing source")
	}
	fe, ok := errors.AsType[*oerrors.FetchError](err)
	if !ok {
		t.Fatalf("want a *FetchError in the chain, got %T: %v", err, err)
	}
	if fe.Kind != oerrors.FetchUnreachable {
		t.Errorf("kind = %s, want unreachable: %v", fe.Kind, err)
	}
	if !isTransientFailure(err) {
		t.Errorf("isTransientFailure(%v) = false, want true", err)
	}
}
