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
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
)

// Platform.spec.skewPolicy is admission-validated by the CRD enum (change
// operator-render-switch, task 1.2): the two documented values are accepted,
// an unset field is accepted (the reconciler reads it as Warn), and any other
// value is rejected by the API server before the operator sees it.
func TestSkewPolicyEnumAtAdmission(t *testing.T) {
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

	platform := func(policy *string) *releasesv1alpha1.Platform {
		return &releasesv1alpha1.Platform{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec: releasesv1alpha1.PlatformSpec{
				Type:       "kubernetes",
				SkewPolicy: policy,
				Registry: map[string]releasesv1alpha1.Subscription{
					"opmodel.dev/catalogs/opm@v4": {Version: "4.0.1"},
				},
			},
		}
	}
	ptr := func(s string) *string { return &s }

	t.Run("unset is admitted", func(t *testing.T) {
		plat := platform(nil)
		require.NoError(t, k8sClient.Create(ctx, plat))
		require.Nil(t, plat.Spec.SkewPolicy, "the API server applies no default; the reconciler resolves nil to Warn")
		require.NoError(t, k8sClient.Delete(ctx, plat))
	})

	for _, policy := range []string{releasesv1alpha1.SkewPolicyWarn, releasesv1alpha1.SkewPolicyRefuse} {
		t.Run(policy+" is admitted", func(t *testing.T) {
			plat := platform(ptr(policy))
			requireEventually(t, func() bool { return k8sClient.Create(ctx, plat) == nil },
				"the previous singleton must be gone before the next create")
			require.Equal(t, policy, *plat.Spec.SkewPolicy)
			require.NoError(t, k8sClient.Delete(ctx, plat))
		})
	}

	t.Run("any other value is rejected", func(t *testing.T) {
		requireEventually(t, func() bool {
			err := k8sClient.Create(ctx, platform(ptr("Strict")))
			return err != nil && apierrors.IsInvalid(err)
		}, "spec.skewPolicy: Strict must be rejected by the enum constraint")
	})
}
