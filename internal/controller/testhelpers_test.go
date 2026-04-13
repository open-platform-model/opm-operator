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
	"fmt"
	"io"
	"os"
	"path/filepath"

	"cuelang.org/go/cue/cuecontext"

	"github.com/open-platform-model/poc-controller/pkg/provider"
)

// stubFetcher returns a fixed error on every Fetch call.
// Use for tests that don't need a working artifact pipeline.
type stubFetcher struct {
	err error
}

func (f *stubFetcher) Fetch(_ context.Context, _, _, _ string) error {
	if f.err != nil {
		return f.err
	}
	return fmt.Errorf("stubFetcher: not implemented")
}

// copyDirFetcher copies a source directory into the fetch target.
// Simulates a successful artifact fetch + extraction.
type copyDirFetcher struct {
	sourceDir string
}

func (f *copyDirFetcher) Fetch(_ context.Context, _, _, dir string) error {
	return copyDir(f.sourceDir, dir)
}

// copyDir recursively copies src into dst.
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}

		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = in.Close() }()

		out, err := os.Create(target)
		if err != nil {
			return err
		}
		defer func() { _ = out.Close() }()

		_, err = io.Copy(out, in)
		return err
	})
}

// testProvider builds a minimal provider for controller tests.
// Produces a ConfigMap from each component's data.message field.
func testProvider() *provider.Provider {
	cueCtx := cuecontext.New()
	data := cueCtx.CompileString(`{
	metadata: {
		name:        "kubernetes"
		description: "Test provider"
		version:     "0.1.0"
	}
	#transformers: {
		"simple": {
			#transform: {
				#component: _
				#context: _
				output: {
					apiVersion: "v1"
					kind:       "ConfigMap"
					metadata: {
						name:      #context.#moduleReleaseMetadata.name
						namespace: #context.#moduleReleaseMetadata.namespace
						labels:    #context.#runtimeLabels
					}
					data: {
						message: #component.data.message
					}
				}
			}
		}
	}
}`)
	if data.Err() != nil {
		panic(fmt.Sprintf("compiling test provider: %v", data.Err()))
	}
	return &provider.Provider{
		Metadata: &provider.ProviderMetadata{
			Name:    "kubernetes",
			Version: "0.1.0",
		},
		Data: data,
	}
}

// testModuleDir returns the path to the render testdata valid-module.
func testModuleDir() string {
	return filepath.Join("..", "render", "testdata", "valid-module")
}
