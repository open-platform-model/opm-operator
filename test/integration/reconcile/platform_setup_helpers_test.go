/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package reconcile_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
)

// useEmptyCUECache points the process CUE_CACHE_DIR at a new, empty
// directory, so the next module-file lookup or build really goes to the
// registry instead of being satisfied from the shared cache. It returns a
// func that restores the previous value; a spec that needs the warm cache
// back mid-test calls it there, and it is also registered with DeferCleanup
// (restoring twice is harmless). The directory is removed at cleanup.
func useEmptyCUECache() (restore func()) {
	GinkgoHelper()
	orig, had := os.LookupEnv("CUE_CACHE_DIR")
	restore = func() {
		if had {
			Expect(os.Setenv("CUE_CACHE_DIR", orig)).To(Succeed())
		} else {
			Expect(os.Unsetenv("CUE_CACHE_DIR")).To(Succeed())
		}
	}
	DeferCleanup(restore)
	// Not GinkgoT().TempDir(): CUE marks extracted cache files read-only,
	// which breaks Ginkgo's automatic removal. Restore write permission
	// before removing, best-effort.
	dir, err := os.MkdirTemp("", "opm-empty-cue-cache-")
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() {
		_ = filepath.WalkDir(dir, func(p string, _ fs.DirEntry, walkErr error) error {
			if walkErr == nil {
				_ = os.Chmod(p, 0o755)
			}
			return nil
		})
		_ = os.RemoveAll(dir)
	})
	Expect(os.Setenv("CUE_CACHE_DIR", dir)).To(Succeed())
	return restore
}

// createClusterPlatform replaces any cluster Platform a sibling spec left
// with one subscribing to catalogPath at the test catalog version, deletes
// it again at cleanup, and returns it with its generation.
func createClusterPlatform(catalogPath string) (*releasesv1alpha1.Platform, int64) {
	GinkgoHelper()
	Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &releasesv1alpha1.Platform{
		ObjectMeta: metav1.ObjectMeta{Name: recoveryPlatformName},
	}))).To(Succeed())
	Eventually(func() bool {
		err := k8sClient.Get(ctx, client.ObjectKey{Name: recoveryPlatformName}, &releasesv1alpha1.Platform{})
		return err != nil && client.IgnoreNotFound(err) == nil
	}).WithTimeout(10 * time.Second).WithPolling(200 * time.Millisecond).Should(BeTrue())

	plat := &releasesv1alpha1.Platform{
		ObjectMeta: metav1.ObjectMeta{Name: recoveryPlatformName},
		Spec: releasesv1alpha1.PlatformSpec{
			Type:     "kubernetes",
			Registry: map[string]releasesv1alpha1.Subscription{catalogPath: {Version: testCatalogVersion()}},
		},
	}
	Expect(k8sClient.Create(ctx, plat)).To(Succeed())
	Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(plat), plat)).To(Succeed())
	Expect(plat.Generation).NotTo(BeZero())
	DeferCleanup(func() {
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &releasesv1alpha1.Platform{
			ObjectMeta: metav1.ObjectMeta{Name: recoveryPlatformName},
		}))).To(Succeed())
	})
	return plat, plat.Generation
}
