package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCodexUsageCommandReadsConfigAndPrintsWeeklyUsage(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	t.Setenv("LENOS_GLOBAL_CONFIG", dir)

	mustWrite(t, configPath, `{
		"providers": {
			"codex": {
				"api_key": "test-token",
				"extra_headers": {"ChatGPT-Account-Id": "account-123"}
			}
		}
	}`)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("missing bearer token")
		}
		if r.Header.Get("ChatGPT-Account-Id") != "account-123" {
			t.Fatalf("missing account ID")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"plan_type": "pro",
			"rate_limit": {
				"secondary_window": {
					"used_percent": 37,
					"reset_at": 1767225600,
					"limit_window_seconds": 604800
				}
			}
		}`))
	}))
	defer server.Close()

	require.NoError(t, codexUsageCmd.Flags().Set("base-url", server.URL))

	var stdout bytes.Buffer
	codexUsageCmd.SetOut(&stdout)

	err := codexUsageCmd.RunE(codexUsageCmd, nil)
	require.NoError(t, err)

	output := stdout.String()
	require.Contains(t, output, "Plan: pro")
	require.Contains(t, output, "Weekly usage: 37% used, 63% left")
	require.Contains(t, output, "Refresh date:")
}

func TestCodexUsageCommandRejectsMissingProvider(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	t.Setenv("LENOS_GLOBAL_CONFIG", dir)

	mustWrite(t, configPath, `{"providers":{}}`)

	err := codexUsageCmd.RunE(codexUsageCmd, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "codex provider not configured")
}

func TestLoadCodexCredentialsParsesOAuth(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	t.Setenv("LENOS_GLOBAL_CONFIG", dir)

	mustWrite(t, configPath, `{
		"providers": {
			"codex": {
				"oauth": {
					"access_token": "oauth-token",
					"refresh_token": "refresh-token"
				},
				"extra_headers": {"ChatGPT-Account-Id": "account-123"}
			}
		}
	}`)

	creds, err := loadCodexCredentials()
	require.NoError(t, err)
	require.Equal(t, "oauth-token", creds.AccessToken)
	require.Equal(t, "account-123", creds.AccountID)
}

func TestLoadCodexCredentialsFallsBackToAPIKey(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	t.Setenv("LENOS_GLOBAL_CONFIG", dir)

	mustWrite(t, configPath, `{
		"providers": {
			"codex": {
				"api_key": "api-key-token"
			}
		}
	}`)

	creds, err := loadCodexCredentials()
	require.NoError(t, err)
	require.Equal(t, "api-key-token", creds.AccessToken)
}

func TestFetchWeeklyUsageSendsAuthHeaders(t *testing.T) {
	var gotAuth, gotAccount string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccount = r.Header.Get("ChatGPT-Account-Id")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"plan_type": "pro",
			"rate_limit": {
				"secondary_window": {
					"used_percent": 37,
					"reset_at": 1767225600,
					"limit_window_seconds": 604800
				}
			}
		}`))
	}))
	defer server.Close()

	usage, err := fetchWeeklyUsage(t.Context(), server.Client(), server.URL, "test-token", "account-123")
	require.NoError(t, err)

	require.Equal(t, "Bearer test-token", gotAuth)
	require.Equal(t, "account-123", gotAccount)
	require.Equal(t, "pro", usage.Plan)
	require.Equal(t, 37, usage.UsedPercent)
	require.Equal(t, 63, usage.RemainingPercent)
}

func TestFetchWeeklyUsageHandlesUnauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	_, err := fetchWeeklyUsage(t.Context(), server.Client(), server.URL, "bad-token", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "token expired")
}

func TestFormatWeeklyUsageIncludesAllFields(t *testing.T) {
	usage := weeklyUsage{
		Plan:             "pro",
		UsedPercent:      42,
		RemainingPercent: 58,
		RefreshAt:        mustParseTime("2026-01-01T00:00:00Z"),
	}
	output := formatWeeklyUsage(usage)
	require.Contains(t, output, "Plan: pro")
	require.Contains(t, output, "Weekly usage: 42% used, 58% left")
	require.Contains(t, output, "Refresh date: 2026-01-01")
}

func TestClampPercentBounds(t *testing.T) {
	require.Equal(t, 0, clampPercent(-5))
	require.Equal(t, 100, clampPercent(150))
	require.Equal(t, 50, clampPercent(50))
}

func TestFetchWeeklyUsageRejectsMissingWindow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"plan_type":"free","rate_limit":{}}`))
	}))
	defer server.Close()

	_, err := fetchWeeklyUsage(t.Context(), server.Client(), server.URL, "token", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "weekly usage window not found")
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

func mustParseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}
