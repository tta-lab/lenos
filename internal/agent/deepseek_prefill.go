package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"charm.land/fantasy"
)

type deepSeekPrefillContextKey struct{}

const deepSeekBetaBaseURL = "https://api.deepseek.com/beta"

type deepSeekPrefillProvider struct {
	inner fantasy.Provider
}

func (p deepSeekPrefillProvider) Name() string {
	return p.inner.Name()
}

func (p deepSeekPrefillProvider) LanguageModel(ctx context.Context, modelID string) (fantasy.LanguageModel, error) {
	model, err := p.inner.LanguageModel(ctx, modelID)
	if err != nil {
		return nil, err
	}
	return deepSeekPrefillModel{inner: model}, nil
}

type deepSeekPrefillModel struct {
	inner fantasy.LanguageModel
}

func (m deepSeekPrefillModel) Generate(ctx context.Context, call fantasy.Call) (*fantasy.Response, error) {
	return m.inner.Generate(ctx, call)
}

func (m deepSeekPrefillModel) Stream(ctx context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	return m.inner.Stream(ctx, call)
}

func (m deepSeekPrefillModel) StreamAssistantPrefill(ctx context.Context, call fantasy.Call, prefill string) (fantasy.StreamResponse, error) {
	prefill = strings.TrimSpace(prefill)
	if prefill == "" {
		return m.inner.Stream(ctx, call)
	}
	call.Prompt = append(append(fantasy.Prompt(nil), call.Prompt...), fantasy.Message{
		Role: fantasy.MessageRoleAssistant,
		Content: []fantasy.MessagePart{
			fantasy.TextPart{Text: prefill},
		},
	})
	ctx = context.WithValue(ctx, deepSeekPrefillContextKey{}, true)
	return m.inner.Stream(ctx, call)
}

func (m deepSeekPrefillModel) GenerateObject(ctx context.Context, call fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return m.inner.GenerateObject(ctx, call)
}

func (m deepSeekPrefillModel) StreamObject(ctx context.Context, call fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return m.inner.StreamObject(ctx, call)
}

func (m deepSeekPrefillModel) Provider() string { return m.inner.Provider() }
func (m deepSeekPrefillModel) Model() string    { return m.inner.Model() }

type deepSeekPrefillTransport struct {
	base http.RoundTripper
}

func (t deepSeekPrefillTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if enabled, _ := req.Context().Value(deepSeekPrefillContextKey{}).(bool); !enabled {
		return t.roundTrip(req)
	}
	if req.Body == nil {
		return t.roundTrip(req)
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	if closeErr := req.Body.Close(); closeErr != nil {
		return nil, closeErr
	}

	body, err = markDeepSeekAssistantPrefix(body)
	if err != nil {
		return nil, err
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(body)), nil
	}
	return t.roundTrip(req)
}

func (t deepSeekPrefillTransport) roundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}

func markDeepSeekAssistantPrefix(body []byte) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	rawMessages, ok := payload["messages"].([]any)
	if !ok || len(rawMessages) == 0 {
		return nil, fmt.Errorf("deepseek prefill request has no messages")
	}
	last, ok := rawMessages[len(rawMessages)-1].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("deepseek prefill last message is not an object")
	}
	if role, _ := last["role"].(string); role != string(fantasy.MessageRoleAssistant) {
		return nil, fmt.Errorf("deepseek prefill last message role is %q, want assistant", role)
	}
	last["prefix"] = true
	payload["messages"] = rawMessages
	return json.Marshal(payload)
}

func supportsDeepSeekPrefill(providerID, baseURL string) bool {
	providerID = strings.ToLower(providerID)
	baseURL = strings.ToLower(baseURL)
	return providerID == "deepseek" ||
		strings.HasPrefix(providerID, "deepseek-") ||
		strings.Contains(baseURL, "api.deepseek.com")
}

func deepSeekPrefillBaseURL(providerID, baseURL string) string {
	switch {
	case strings.Contains(strings.ToLower(baseURL), "api.deepseek.com"):
		return deepSeekBetaBaseURL
	case baseURL == "" && supportsDeepSeekPrefill(providerID, baseURL):
		return deepSeekBetaBaseURL
	default:
		return baseURL
	}
}
