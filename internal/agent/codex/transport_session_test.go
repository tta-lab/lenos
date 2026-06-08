package codex_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tta-lab/lenos/internal/agent/codex"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestWithSessionID_RoundTrip_InjectsPromptCacheKey(t *testing.T) {
	t.Parallel()

	wantSID := "session-uuid-12345"

	client := codex.NewClient(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		require.NoError(t, err)

		var m map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(body, &m))

		rawKey, ok := m["prompt_cache_key"]
		require.True(t, ok, "prompt_cache_key should be present in body")
		var key string
		require.NoError(t, json.Unmarshal(rawKey, &key))
		require.Equal(t, wantSID, key)

		_, ok = m["max_output_tokens"]
		require.False(t, ok, "max_output_tokens should be stripped")

		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader("{}")),
		}, nil
	}))

	bodyJSON := `{"max_output_tokens": 4096, "model": "gpt-5.5", "input": []}`
	req, err := http.NewRequestWithContext(
		codex.WithSessionID(t.Context(), wantSID),
		http.MethodPost,
		"https://chatgpt.com/backend-api/codex/responses",
		bytes.NewReader([]byte(bodyJSON)),
	)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
}

func TestWithSessionID_RoundTrip_InjectsHeaders(t *testing.T) {
	t.Parallel()

	wantSID := "session-uuid-67890"

	client := codex.NewClient(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		require.Equal(t, wantSID, req.Header.Get("x-client-request-id"))
		require.Equal(t, wantSID, req.Header.Get("session_id"))
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader("{}")),
		}, nil
	}))

	bodyJSON := `{"model": "gpt-5.5", "input": []}`
	req, err := http.NewRequestWithContext(
		codex.WithSessionID(t.Context(), wantSID),
		http.MethodPost,
		"https://chatgpt.com/backend-api/codex/responses",
		bytes.NewReader([]byte(bodyJSON)),
	)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
}

func TestWithSessionID_RoundTrip_NoSessionID_NoInjection(t *testing.T) {
	t.Parallel()

	client := codex.NewClient(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		require.NoError(t, err)

		var m map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(body, &m))
		_, ok := m["prompt_cache_key"]
		require.False(t, ok, "prompt_cache_key should not be present without session ID")

		require.Empty(t, req.Header.Get("x-client-request-id"))
		require.Empty(t, req.Header.Get("session_id"))

		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader("{}")),
		}, nil
	}))

	bodyJSON := `{"model": "gpt-5.5", "input": []}`
	req, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		"https://chatgpt.com/backend-api/codex/responses",
		bytes.NewReader([]byte(bodyJSON)),
	)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
}

func TestWithSessionID_RoundTrip_EmptySessionID_NoInjection(t *testing.T) {
	t.Parallel()

	client := codex.NewClient(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(req.Body)
		var m map[string]json.RawMessage
		json.Unmarshal(body, &m)
		_, ok := m["prompt_cache_key"]
		require.False(t, ok)

		require.Empty(t, req.Header.Get("x-client-request-id"))
		require.Empty(t, req.Header.Get("session_id"))
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader("{}")),
		}, nil
	}))

	bodyJSON := `{"model": "gpt-5.5", "input": []}`
	req, err := http.NewRequestWithContext(
		codex.WithSessionID(t.Context(), ""),
		http.MethodPost,
		"https://chatgpt.com/backend-api/codex/responses",
		bytes.NewReader([]byte(bodyJSON)),
	)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
}

func TestWithSessionID_RoundTrip_PreserveExistingPromptCacheKey(t *testing.T) {
	t.Parallel()

	existingSID := "already-set-id"
	ctxSID := "should-not-override"

	client := codex.NewClient(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		require.NoError(t, err)

		var m map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(body, &m))

		rawKey, ok := m["prompt_cache_key"]
		require.True(t, ok)
		var key string
		require.NoError(t, json.Unmarshal(rawKey, &key))
		require.Equal(t, existingSID, key, "should preserve existing prompt_cache_key")

		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader("{}")),
		}, nil
	}))

	bodyJSON := `{"prompt_cache_key": "already-set-id", "model": "gpt-5.5", "input": []}`
	req, err := http.NewRequestWithContext(
		codex.WithSessionID(t.Context(), ctxSID),
		http.MethodPost,
		"https://chatgpt.com/backend-api/codex/responses",
		bytes.NewReader([]byte(bodyJSON)),
	)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	require.NoError(t, err)
	resp.Body.Close()
}
