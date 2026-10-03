/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package reconcile_test

import (
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync/atomic"

	"cuelang.org/go/mod/modconfig"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// flakyFront is an HTTP front over the real registry that serves one
// catalog. While down it answers every request 503; once switched to
// forwarding it reverse-proxies to the upstream registry host. It lets a
// spec fail a real registry fetch and then let the same fetch succeed,
// with no change to the code under test's configuration.
type flakyFront struct {
	// Registry is the live mapping plus one longest-prefix entry that sends
	// only the catalog's module path through the front. Core and every other
	// module still resolve from the live registry.
	Registry string

	forwarding atomic.Bool
	refused    atomic.Int64
	forwarded  atomic.Int64
}

// newFlakyFront starts a front, down, for catalogPath (module path with its
// major version) under the liveRegistry mapping, and closes it at cleanup.
//
// The front's mapping entry carries the upstream's repository prefix, so the
// repository the client asks the front for is the upstream's byte for byte:
// no path rewrite is needed, and bearer-token scopes (fetched by the client
// straight from the upstream's absolute realm URL) match.
func newFlakyFront(liveRegistry, catalogPath string) *flakyFront {
	GinkgoHelper()
	basePath, _, _ := strings.Cut(catalogPath, "@")

	live, err := modconfig.NewResolver(&modconfig.Config{CUERegistry: liveRegistry})
	Expect(err).NotTo(HaveOccurred())
	loc, ok := live.ResolveToLocation(basePath, "")
	Expect(ok).To(BeTrue(), "the live mapping must resolve %s", basePath)
	upstream := &url.URL{Scheme: "https", Host: loc.Host}
	if loc.Insecure {
		upstream.Scheme = "http"
	}

	f := &flakyFront{}
	proxy := &httputil.ReverseProxy{Rewrite: func(pr *httputil.ProxyRequest) { pr.SetURL(upstream) }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !f.forwarding.Load() {
			f.refused.Add(1)
			http.Error(w, "registry unavailable", http.StatusServiceUnavailable)
			return
		}
		f.forwarded.Add(1)
		proxy.ServeHTTP(w, req)
	}))
	DeferCleanup(srv.Close)

	// A simple mapping appends the module path to the repository prefix, so
	// the front's prefix is the upstream repository minus the module path.
	prefix, found := strings.CutSuffix(loc.Repository, basePath)
	Expect(found).To(BeTrue(), "upstream repository %q does not end in %q", loc.Repository, basePath)
	frontHost := strings.TrimPrefix(srv.URL, "http://")
	if prefix = strings.TrimSuffix(prefix, "/"); prefix != "" {
		frontHost += "/" + prefix
	}
	for entry := range strings.SplitSeq(liveRegistry, ",") {
		Expect(strings.HasPrefix(strings.TrimSpace(entry), basePath+"=")).To(BeFalse(),
			"the live CUE_REGISTRY already maps %q; the front cannot add its own entry", basePath)
	}
	f.Registry = liveRegistry + "," + basePath + "=" + frontHost + "+insecure"

	fronted, err := modconfig.NewResolver(&modconfig.Config{CUERegistry: f.Registry})
	Expect(err).NotTo(HaveOccurred())
	floc, ok := fronted.ResolveToLocation(basePath, "")
	Expect(ok).To(BeTrue())
	Expect(floc.Repository).To(Equal(loc.Repository), "the front must serve the upstream repository unchanged")
	return f
}

// Forward switches the front from refusing to forwarding.
func (f *flakyFront) Forward() { f.forwarding.Store(true) }

// Refused counts the requests answered 503 while down.
func (f *flakyFront) Refused() int64 { return f.refused.Load() }

// Forwarded counts the requests passed to the upstream.
func (f *flakyFront) Forwarded() int64 { return f.forwarded.Load() }
