package core

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/atlantis-mk/FastProxy-Server/internal/repository"
)

func TestHealthCheckSendsSecretAuthorization(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/version" {
			t.Fatalf("path = %q, want /version", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-secret" {
			t.Fatalf("Authorization = %q, want Bearer test-secret", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	adapter := Mihomo{}
	if err := adapter.HealthCheck(context.Background(), RuntimeConfig{
		ExternalController: server.Listener.Addr().String(),
		Secret:             " test-secret ",
	}); err != nil {
		t.Fatalf("HealthCheck() error = %v", err)
	}
}

func TestHealthCheckOmitsBlankSecretAuthorization(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Fatalf("Authorization = %q, want empty", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	adapter := SingBox{}
	if adapter.Core() != repository.CoreSingBox {
		t.Fatalf("Core() = %q, want %q", adapter.Core(), repository.CoreSingBox)
	}
	if err := adapter.HealthCheck(context.Background(), RuntimeConfig{
		ExternalController: server.Listener.Addr().String(),
		Secret:             "  ",
	}); err != nil {
		t.Fatalf("HealthCheck() error = %v", err)
	}
}
