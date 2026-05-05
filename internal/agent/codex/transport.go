package codex

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

// NewClient returns an http.Client whose transport strips Codex-incompatible
// fields (`max_output_tokens`) from outgoing JSON request bodies.
//
// Why: fantasy/openai's responses path defaults Store=false and (because
// `gpt-5-codex` etc. are detected as reasoning models by getResponsesModelConfig)
// already strips Temperature. But MaxOutputTokens is sent whenever
// call.MaxOutputTokens != nil — and the lenos coordinator may pass MaxTokens
// through. The Codex backend rejects requests containing max_output_tokens, so
// this wrapper strips it defensively. Mirrors copilot.NewClient pattern.
//
// If `wrap` is nil, http.DefaultTransport is used.
func NewClient(wrap http.RoundTripper) *http.Client {
	if wrap == nil {
		wrap = http.DefaultTransport
	}
	return &http.Client{
		Transport: &stripTransport{wrap: wrap},
	}
}

type stripTransport struct {
	wrap http.RoundTripper
}

func (t *stripTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body == nil || req.Body == http.NoBody {
		return t.wrap.RoundTrip(req)
	}
	if req.Header.Get("Content-Type") != "application/json" &&
		!strings.HasPrefix(req.Header.Get("Content-Type"), "application/json") {
		return t.wrap.RoundTrip(req)
	}

	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()

	stripped := stripJSONField(bodyBytes, "max_output_tokens")

	req = req.Clone(req.Context())
	req.Body = io.NopCloser(bytes.NewReader(stripped))
	req.ContentLength = int64(len(stripped))
	return t.wrap.RoundTrip(req)
}

// stripJSONField removes a top-level field from a JSON object body.
// Returns the original bytes on parse error (defensive — don't break the request
// if it's not JSON we recognize).
func stripJSONField(body []byte, field string) []byte {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return body
	}
	if _, ok := m[field]; !ok {
		return body
	}
	delete(m, field)
	out, err := json.Marshal(m)
	if err != nil {
		slog.Warn("codex transport: failed to re-marshal stripped body", "err", err)
		return body
	}
	return out
}
