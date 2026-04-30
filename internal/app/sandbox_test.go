package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tta-lab/lenos/internal/config"
)

// newHealthyServer mocks the temenos daemon /health endpoint per the contract
// in temenos v0.7.0 (client/client.go:163-184): Health() does GET /health and
// only checks for HTTP 200 — it does not parse the response body. This mock
// returns the same JSON shape the real daemon emits (HealthResponse in
// temenos/internal/daemon/handler.go:48-53) so the contract stays traceable
// if the temenos client ever starts validating the body.
//
// If the temenos client API changes (e.g. Health() starts requiring fields),
// update this mock — and add a regression test covering the new contract.
func newHealthyServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "platform": "test", "version": "test"})
	})
	return httptest.NewServer(mux)
}

func ptrBool(b bool) *bool { return &b }

func TestInitSandboxClient(t *testing.T) {
	t.Parallel()
	t.Run("nil + healthy server returns client", func(t *testing.T) {
		t.Parallel()
		s := newHealthyServer(t)
		defer s.Close()
		c, err := initSandboxClient(context.Background(), &config.Options{SandboxEndpoint: s.URL})
		require.NoError(t, err)
		require.NotNil(t, c)
	})

	t.Run("nil + dead server returns nil client and nil error (soft fallback)", func(t *testing.T) {
		t.Parallel()
		s := newHealthyServer(t)
		s.Close()
		c, err := initSandboxClient(context.Background(), &config.Options{SandboxEndpoint: s.URL})
		require.NoError(t, err)
		require.Nil(t, c)
	})

	t.Run("explicit true + healthy server returns client", func(t *testing.T) {
		t.Parallel()
		s := newHealthyServer(t)
		defer s.Close()
		c, err := initSandboxClient(context.Background(), &config.Options{
			Sandbox: ptrBool(true), SandboxEndpoint: s.URL,
		})
		require.NoError(t, err)
		require.NotNil(t, c)
	})

	t.Run("explicit true + dead server returns error", func(t *testing.T) {
		t.Parallel()
		s := newHealthyServer(t)
		s.Close()
		c, err := initSandboxClient(context.Background(), &config.Options{
			Sandbox: ptrBool(true), SandboxEndpoint: s.URL,
		})
		require.Error(t, err)
		require.Nil(t, c)
		require.Contains(t, err.Error(), "sandbox=true but temenos daemon unreachable")
	})

	t.Run("explicit false skips probe entirely", func(t *testing.T) {
		t.Parallel()
		c, err := initSandboxClient(context.Background(), &config.Options{
			Sandbox: ptrBool(false), SandboxEndpoint: "http://invalid.invalid:1",
		})
		require.NoError(t, err)
		require.Nil(t, c)
	})
}
