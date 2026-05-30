package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"charm.land/fantasy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMarkDeepSeekAssistantPrefix(t *testing.T) {
	t.Parallel()

	body := []byte(`{"model":"deepseek-chat","messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"` + messageBlockPrefillToken + `"}]}`)

	got, err := markDeepSeekAssistantPrefix(body)

	require.NoError(t, err)
	var payload struct {
		Messages []map[string]any `json:"messages"`
	}
	require.NoError(t, json.Unmarshal(got, &payload))
	require.Len(t, payload.Messages, 2)
	assert.Equal(t, true, payload.Messages[1]["prefix"])
	assert.Equal(t, messageBlockPrefillToken, payload.Messages[1]["content"])
}

func TestMarkDeepSeekAssistantPrefixRequiresAssistantLast(t *testing.T) {
	t.Parallel()

	_, err := markDeepSeekAssistantPrefix([]byte(`{"messages":[{"role":"user","content":"hi"}]}`))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "want assistant")
}

func TestDeepSeekPrefillModelAppendsAssistantPrefixMessage(t *testing.T) {
	t.Parallel()

	inner := &streamCapturingModel{inner: &scriptedModel{emits: []string{"exit"}}}
	model := deepSeekPrefillModel{inner: inner}
	_, err := model.StreamAssistantPrefill(context.Background(), fantasy.Call{
		Prompt: fantasy.Prompt{fantasy.NewUserMessage("hi")},
	}, messageBlockPrefillToken)

	require.NoError(t, err)
	require.Len(t, inner.captured, 1)
	prompt := inner.captured[0]
	require.Len(t, prompt, 2)
	assert.Equal(t, fantasy.MessageRoleAssistant, prompt[1].Role)
	assert.Equal(t, messageBlockPrefillToken, fantasyMessageText(prompt[1]))
}

func TestDeepSeekPrefillTransportMarksRequestFromContext(t *testing.T) {
	t.Parallel()

	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		require.NoError(t, err)

		var payload struct {
			Messages []map[string]any `json:"messages"`
		}
		require.NoError(t, json.Unmarshal(body, &payload))
		assert.Equal(t, true, payload.Messages[1]["prefix"])

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader([]byte(`{}`))),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})
	transport := deepSeekPrefillTransport{base: base}
	body := []byte(`{"messages":[{"role":"user","content":"hi"},{"role":"assistant","content":"` + messageBlockPrefillToken + `"}]}`)
	req := httptestRequest(t, context.WithValue(context.Background(), deepSeekPrefillContextKey{}, true), body)

	resp, err := transport.RoundTrip(req)

	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestSupportsDeepSeekPrefillOnlyForDeepSeek(t *testing.T) {
	t.Parallel()

	assert.True(t, supportsDeepSeekPrefill("deepseek", "https://example.com/v1"))
	assert.True(t, supportsDeepSeekPrefill("deepseek-main", "https://example.com/v1"))
	assert.True(t, supportsDeepSeekPrefill("custom", "https://api.deepseek.com/beta"))
	assert.False(t, supportsDeepSeekPrefill("openai", "https://api.openai.com/v1"))
	assert.False(t, supportsDeepSeekPrefill("openrouter", "https://openrouter.ai/api/v1"))
}

func TestDeepSeekPrefillBaseURLUsesBetaForOfficialAPI(t *testing.T) {
	t.Parallel()

	assert.Equal(t, deepSeekBetaBaseURL, deepSeekPrefillBaseURL("deepseek", "https://api.deepseek.com/v1"))
	assert.Equal(t, deepSeekBetaBaseURL, deepSeekPrefillBaseURL("deepseek", ""))
	assert.Equal(t, "https://proxy.example.com/v1", deepSeekPrefillBaseURL("deepseek", "https://proxy.example.com/v1"))
	assert.Equal(t, "https://api.openai.com/v1", deepSeekPrefillBaseURL("openai", "https://api.openai.com/v1"))
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func httptestRequest(t *testing.T, ctx context.Context, body []byte) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.deepseek.com/beta/chat/completions", bytes.NewReader(body))
	require.NoError(t, err)
	return req
}
