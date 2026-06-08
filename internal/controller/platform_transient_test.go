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
	"net"
	"net/url"
	"testing"

	oerrors "github.com/open-platform-model/library/opm/errors"
)

// timeoutError is a net.Error whose Timeout() is true, modelling a dial/read
// deadline reached against the registry.
type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

var _ net.Error = timeoutError{}

func TestIsTransientMaterialize(t *testing.T) {
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
			name: "net.Error with Timeout is transient",
			err:  timeoutError{},
			want: true,
		},
		{
			name: "url.Error (registry unreachable) is transient",
			err:  &url.Error{Op: "Get", URL: "https://registry.invalid/v2/", Err: errors.New("connection refused")},
			want: true,
		},
		{
			name: "context deadline exceeded is transient",
			err:  context.DeadlineExceeded,
			want: true,
		},
		{
			name: "transient cause wrapped in MaterializeError is transient",
			err: &oerrors.MaterializeError{
				Kind:         oerrors.MaterializeKindCatalog,
				Subscription: "testing.opmodel.dev/catalogs/example",
				Cause:        &url.Error{Op: "Get", URL: "https://registry.invalid/v2/", Err: errors.New("connection refused")},
			},
			want: true,
		},
		{
			name: "semantic MaterializeError (path not found) is not transient",
			err: &oerrors.MaterializeError{
				Kind:         oerrors.MaterializeKindCatalog,
				Subscription: "testing.opmodel.dev/catalogs/does-not-exist",
				Cause:        errors.New("subscription path could not be resolved"),
			},
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
			if got := isTransientMaterialize(tt.err); got != tt.want {
				t.Errorf("isTransientMaterialize(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
