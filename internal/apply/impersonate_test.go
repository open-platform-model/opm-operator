package apply

import (
	"context"
	"errors"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestBuildImpersonationConfig_NamesTheServiceAccountOnly(t *testing.T) {
	cfg := buildImpersonationConfig("team-a", "deploy-sa")

	const wantUser = "system:serviceaccount:team-a:deploy-sa"
	if cfg.UserName != wantUser {
		t.Fatalf("UserName = %q, want %q", cfg.UserName, wantUser)
	}

	// A group here would need the impersonate verb on groups, which the
	// manager role does not hold; the apiserver derives the groups.
	if len(cfg.Groups) != 0 {
		t.Fatalf("Groups = %v, want none", cfg.Groups)
	}
	if cfg.UID != "" || len(cfg.Extra) != 0 {
		t.Fatalf("UID = %q, Extra = %v, want neither", cfg.UID, cfg.Extra)
	}
}

func TestIsServiceAccountNotFound(t *testing.T) {
	notFound := apierrors.NewNotFound(schema.GroupResource{Resource: "serviceaccounts"}, "deploy-sa")
	forbidden := apierrors.NewForbidden(schema.GroupResource{Resource: "serviceaccounts"}, "deploy-sa", errors.New("no impersonate"))

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil returns false", nil, false},
		{"bare NotFound returns true", notFound, true},
		{"wrapped NotFound returns true", fmt.Errorf("serviceAccount team-a/deploy-sa not found: %w", notFound), true},
		{"doubly-wrapped NotFound returns true", fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", notFound)), true},
		{"bare Forbidden returns false", forbidden, false},
		{"wrapped Forbidden returns false", fmt.Errorf("denied: %w", forbidden), false},
		{"sentinel error returns false", errors.New("generic failure"), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsServiceAccountNotFound(tc.err); got != tc.want {
				t.Fatalf("IsServiceAccountNotFound(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestNewImpersonatedClient(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "deploy-sa",
			Namespace: "team-a",
		},
	}

	t.Run("returns error when SA not found", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		cfg := &rest.Config{Host: "https://localhost:6443"}

		_, err := NewImpersonatedClient(context.Background(), cfg, c, scheme, "team-a", "nonexistent")
		if err == nil {
			t.Fatal("expected error for missing SA, got nil")
		}
	})

	t.Run("builds client with correct impersonation config", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(sa).Build()
		cfg := &rest.Config{Host: "https://localhost:6443"}

		impClient, err := NewImpersonatedClient(context.Background(), cfg, c, scheme, "team-a", "deploy-sa")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if impClient == nil {
			t.Fatal("expected non-nil client")
		}
	})

	t.Run("SA-NotFound error is detected by IsServiceAccountNotFound", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).Build()
		cfg := &rest.Config{Host: "https://localhost:6443"}

		_, err := NewImpersonatedClient(context.Background(), cfg, c, scheme, "team-a", "nonexistent")
		if err == nil {
			t.Fatal("expected error for missing SA, got nil")
		}
		if !IsServiceAccountNotFound(err) {
			t.Fatalf("IsServiceAccountNotFound(%v) = false, want true", err)
		}
	})

	t.Run("does not mutate original config", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(sa).Build()
		cfg := &rest.Config{Host: "https://localhost:6443"}

		_, err := NewImpersonatedClient(context.Background(), cfg, c, scheme, "team-a", "deploy-sa")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Impersonate.UserName != "" {
			t.Fatalf("original config was mutated: Impersonate.UserName = %q", cfg.Impersonate.UserName)
		}
	})
}
