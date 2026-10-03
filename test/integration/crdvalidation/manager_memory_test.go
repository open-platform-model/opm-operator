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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"
)

// The shipped installer pairs the manager's memory limit with a Go soft
// limit of about 80% of it. The soft limit is a literal (the downward API
// cannot scale a value), so nothing keeps the two in step but this test: a
// limit lowered below GOMEMLIMIT silently disables the soft limit.
func TestInstallerManagerMemoryLimits(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "dist", "install.yaml"))
	require.NoError(t, err)

	var manager *corev1.Container
	for doc := range strings.SplitSeq(string(raw), "\n---\n") {
		if !strings.Contains(doc, "kind: Deployment") {
			continue
		}
		var deploy appsv1.Deployment
		require.NoError(t, yaml.Unmarshal([]byte(doc), &deploy))
		for i, c := range deploy.Spec.Template.Spec.Containers {
			if c.Name == "manager" {
				manager = &deploy.Spec.Template.Spec.Containers[i]
			}
		}
	}
	require.NotNil(t, manager, "dist/install.yaml has a Deployment with a manager container")

	limit := manager.Resources.Limits[corev1.ResourceMemory]
	require.Equal(t, "4Gi", limit.String())

	var gomemlimit string
	for _, e := range manager.Env {
		if e.Name == "GOMEMLIMIT" {
			gomemlimit = e.Value
		}
	}
	require.Equal(t, "3276MiB", gomemlimit, "GOMEMLIMIT is about 80% of limits.memory; move both together")
}
