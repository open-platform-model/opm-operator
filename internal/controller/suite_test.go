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
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	releasesv1alpha1 "github.com/open-platform-model/opm-operator/api/v1alpha1"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	ctx       context.Context
	cancel    context.CancelFunc
	testEnv   *envtest.Environment
	cfg       *rest.Config
	k8sClient client.Client
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	var err error
	err = releasesv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	err = sourcev1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// +kubebuilder:scaffold:scheme

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "config", "crd", "bases"),
			findSourceControllerCRDs(),
		},
		ErrorIfCRDPathMissing: true,
	}

	// Retrieve the first found binary directory to allow running tests from IDEs
	if getFirstFoundEnvTestBinaryDir() != "" {
		testEnv.BinaryAssetsDirectory = getFirstFoundEnvTestBinaryDir()
	}

	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	cancel()
	Eventually(func() error {
		return testEnv.Stop()
	}, time.Minute, time.Second).Should(Succeed())
})

// findSourceControllerCRDs returns the path to the Flux source-controller
// CRD bases within the Go module cache so envtest can install GitRepository,
// OCIRepository, and Bucket CRDs for Release reconciler tests.
//
// The repo only depends on github.com/fluxcd/source-controller/api (a sub-module
// without CRDs). The parent source-controller module ships the CRDs and must be
// fetched into the module cache separately. We pin to the same version as the
// /api sub-module since flux releases them together.
// Panics if the directory cannot be located — tests cannot run without it.
func findSourceControllerCRDs() string {
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		gopath = filepath.Join(os.Getenv("HOME"), "go")
	}
	pattern := filepath.Join(gopath, "pkg", "mod", "github.com", "fluxcd", "source-controller@*", "config", "crd", "bases")
	if matches, _ := filepath.Glob(pattern); len(matches) > 0 {
		return matches[len(matches)-1]
	}
	ver, err := exec.Command("go", "list", "-m", "-f", "{{.Version}}", "github.com/fluxcd/source-controller/api").Output()
	if err != nil {
		panic(fmt.Sprintf("resolving source-controller/api version: %v", err))
	}
	version := string(bytes.TrimSpace(ver))
	out, err := exec.Command("go", "mod", "download", "github.com/fluxcd/source-controller@"+version).CombinedOutput()
	if err != nil {
		panic(fmt.Sprintf("downloading source-controller@%s: %v\n%s", version, err, out))
	}
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		panic(fmt.Sprintf("source-controller CRDs not found under %s after `go mod download github.com/fluxcd/source-controller@%s`", gopath, version))
	}
	return matches[len(matches)-1]
}

// getFirstFoundEnvTestBinaryDir locates the first binary in the specified path.
// ENVTEST-based tests depend on specific binaries, usually located in paths set by
// controller-runtime. When running tests directly (e.g., via an IDE) without using
// Taskfile targets, the 'BinaryAssetsDirectory' must be explicitly configured.
//
// This function streamlines the process by finding the required binaries, similar to
// setting the 'KUBEBUILDER_ASSETS' environment variable. To ensure the binaries are
// properly set up, run 'task dev:test' once beforehand (it provisions envtest via
// the :tool:setup-envtest dependency).
func getFirstFoundEnvTestBinaryDir() string {
	basePath := filepath.Join("..", "..", "bin", "k8s")
	entries, err := os.ReadDir(basePath)
	if err != nil {
		logf.Log.Error(err, "Failed to read directory", "path", basePath)
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(basePath, entry.Name())
		}
	}
	return ""
}
