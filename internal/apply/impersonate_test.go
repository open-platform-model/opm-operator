package apply

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

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

		_, err := NewImpersonatedClient(context.Background(), cfg, c, "team-a", "nonexistent")
		if err == nil {
			t.Fatal("expected error for missing SA, got nil")
		}
	})

	t.Run("builds client with correct impersonation config", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(sa).Build()
		cfg := &rest.Config{Host: "https://localhost:6443"}

		impClient, err := NewImpersonatedClient(context.Background(), cfg, c, "team-a", "deploy-sa")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if impClient == nil {
			t.Fatal("expected non-nil client")
		}
	})

	t.Run("does not mutate original config", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(sa).Build()
		cfg := &rest.Config{Host: "https://localhost:6443"}

		_, err := NewImpersonatedClient(context.Background(), cfg, c, "team-a", "deploy-sa")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Impersonate.UserName != "" {
			t.Fatalf("original config was mutated: Impersonate.UserName = %q", cfg.Impersonate.UserName)
		}
	})
}
