package apply

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// apiRequest is what the stub API server keeps of one request.
type apiRequest struct {
	method, path, dryRun, impersonateUser string
	impersonateGroups                     []string
}

// stubAPIServer serves the discovery a ConfigMap needs, answers every read of
// an object with NotFound and every write with patchStatus, and records each
// request.
func stubAPIServer(t *testing.T, patchStatus int) (*httptest.Server, func() []apiRequest) {
	t.Helper()
	var (
		mu   sync.Mutex
		seen []apiRequest
	)
	writeJSON := func(w http.ResponseWriter, code int, body any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		if err := json.NewEncoder(w).Encode(body); err != nil {
			t.Errorf("encoding the response: %v", err)
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, apiRequest{
			method:            r.Method,
			path:              r.URL.Path,
			dryRun:            r.URL.Query().Get("dryRun"),
			impersonateUser:   r.Header.Get("Impersonate-User"),
			impersonateGroups: r.Header.Values("Impersonate-Group"),
		})
		mu.Unlock()

		switch {
		case r.URL.Path == "/api":
			writeJSON(w, http.StatusOK, metav1.APIVersions{
				TypeMeta: metav1.TypeMeta{Kind: "APIVersions"}, Versions: []string{"v1"},
			})
		case r.URL.Path == "/apis":
			writeJSON(w, http.StatusOK, metav1.APIGroupList{TypeMeta: metav1.TypeMeta{Kind: "APIGroupList"}})
		case r.URL.Path == "/api/v1":
			writeJSON(w, http.StatusOK, metav1.APIResourceList{
				TypeMeta:     metav1.TypeMeta{Kind: "APIResourceList"},
				GroupVersion: "v1",
				APIResources: []metav1.APIResource{{
					Name: "configmaps", Namespaced: true, Kind: "ConfigMap",
					Verbs: metav1.Verbs{"get", "patch"},
				}},
			})
		case r.Method == http.MethodPatch && patchStatus == http.StatusForbidden:
			writeJSON(w, patchStatus, metav1.Status{
				TypeMeta: metav1.TypeMeta{Kind: "Status", APIVersion: "v1"},
				Status:   metav1.StatusFailure,
				Reason:   metav1.StatusReasonForbidden,
				Code:     http.StatusForbidden,
				Message:  `configmaps "app" is forbidden: User "system:serviceaccount:team-a:deploy-sa" cannot patch resource "configmaps"`,
			})
		default:
			writeJSON(w, http.StatusNotFound, metav1.Status{
				TypeMeta: metav1.TypeMeta{Kind: "Status", APIVersion: "v1"},
				Status:   metav1.StatusFailure,
				Reason:   metav1.StatusReasonNotFound,
				Code:     http.StatusNotFound,
			})
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() []apiRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]apiRequest(nil), seen...)
	}
}

// Drift detection through the client NewImpersonatedClient builds sends its
// dry-run as the ServiceAccount, and a refused dry-run comes back as an error
// that still reads as Forbidden, which is what the reconciler reports.
func TestDetectDrift_DryRunsAsTheImpersonatedServiceAccount(t *testing.T) {
	srv, requests := stubAPIServer(t, http.StatusForbidden)

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: "deploy-sa", Namespace: "team-a"},
	}).Build()

	impClient, err := NewImpersonatedClient(context.Background(), &rest.Config{Host: srv.URL}, reader, scheme, "team-a", "deploy-sa")
	if err != nil {
		t.Fatalf("building the impersonated client: %v", err)
	}
	rm := NewResourceManager(impClient, "opm-controller")

	cm := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]any{"name": "app", "namespace": "team-a"},
		"data":       map[string]any{"key": "value"},
	}}
	result, err := DetectDrift(context.Background(), rm, []*unstructured.Unstructured{cm})
	if err == nil {
		t.Fatalf("DetectDrift = %+v, want the refusal as an error, never a verdict", result)
	}
	if !apierrors.IsForbidden(err) {
		t.Fatalf("apierrors.IsForbidden(%v) = false, want true", err)
	}
	if !strings.Contains(err.Error(), "system:serviceaccount:team-a:deploy-sa") {
		t.Fatalf("error %q does not name the refused identity", err)
	}

	const wantUser = "system:serviceaccount:team-a:deploy-sa"
	var dryRuns int
	for _, req := range requests() {
		if req.impersonateUser != wantUser {
			t.Errorf("%s %s: Impersonate-User = %q, want %q", req.method, req.path, req.impersonateUser, wantUser)
		}
		if len(req.impersonateGroups) != 0 {
			t.Errorf("%s %s: Impersonate-Group = %v, want none", req.method, req.path, req.impersonateGroups)
		}
		if req.method == http.MethodPatch {
			dryRuns++
			if req.dryRun != metav1.DryRunAll {
				t.Errorf("PATCH %s: dryRun = %q, want %q: drift detection must not write", req.path, req.dryRun, metav1.DryRunAll)
			}
			if req.path != "/api/v1/namespaces/team-a/configmaps/app" {
				t.Errorf("PATCH path = %q", req.path)
			}
		}
	}
	if dryRuns != 1 {
		t.Fatalf("saw %d dry-run PATCH requests, want 1", dryRuns)
	}
}
