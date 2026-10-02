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

package crdvalidation_test

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/yaml"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
)

// Every opmodel.dev object in config/samples is admitted by the CRDs as they
// are: a server-side dry-run create with strict field validation, so a
// sample carrying a field the schema dropped fails here instead of being
// pruned silently. The operator resource reference
// (docs/site/reference/operator-resources.md) shows these samples as its
// examples, so this is what keeps them current with the schema.
func TestSamplesAdmittedByTheCRDs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}
	if dir := firstEnvtestBinaryDir(t); dir != "" {
		testEnv.BinaryAssetsDirectory = dir
	}
	cfg, err := testEnv.Start()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, testEnv.Stop())
	})
	require.NoError(t, releasesv1alpha1.AddToScheme(scheme.Scheme))
	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	require.NoError(t, err)

	files, err := filepath.Glob(filepath.Join("..", "..", "..", "config", "samples", "*.yaml"))
	require.NoError(t, err)
	seen := 0
	for _, f := range files {
		for _, obj := range sampleObjects(t, f) {
			if obj.GroupVersionKind().GroupVersion() != releasesv1alpha1.GroupVersion {
				continue
			}
			seen++
			t.Run(filepath.Base(f)+"/"+obj.GetKind()+"/"+obj.GetName(), func(t *testing.T) {
				require.NoError(t, k8sClient.Create(ctx, obj, client.DryRunAll, client.FieldValidation("Strict")))
			})
		}
	}
	require.NotZero(t, seen, "config/samples holds no opmodel.dev object")
}

func sampleObjects(t *testing.T, path string) []*unstructured.Unstructured {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	reader := utilyaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))
	var out []*unstructured.Unstructured
	for {
		doc, err := reader.Read()
		if errors.Is(err, io.EOF) {
			return out
		}
		require.NoError(t, err)
		obj := map[string]any{}
		require.NoError(t, yaml.Unmarshal(doc, &obj))
		if len(obj) == 0 {
			continue
		}
		out = append(out, &unstructured.Unstructured{Object: obj})
	}
}
