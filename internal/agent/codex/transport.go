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
	stripped = hoistSystemToInstructions(stripped)

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

// hoistSystemToInstructions hoists the first system/developer-role item from
// input[] into the top-level "instructions" field, removing it from input[].
//
// Why: ChatGPT-OAuth Codex backend at chatgpt.com/backend-api/codex requires
// the top-level "instructions" field. Fantasy emits the system prompt as a
// developer-role item inside input[] for codex models (gpt-5.x match the
// reasoning-model path with systemMessageMode="developer"). This transport
// rewrites the body shape the backend expects.
//
// Behavior:
//   - Passes body through unchanged on parse error or unrecognized shape.
//   - Passes through if "instructions" is already set (don't overwrite caller intent).
//   - Hoists only the FIRST matching item; later system/developer items remain.
//   - Handles both string content and list-of-input_text content forms.
func hoistSystemToInstructions(body []byte) []byte {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return body
	}
	if _, ok := m["instructions"]; ok {
		return body
	}
	rawInput, ok := m["input"]
	if !ok {
		return body
	}
	var inputArr []json.RawMessage
	if err := json.Unmarshal(rawInput, &inputArr); err != nil {
		// input is a string or non-array; leave alone
		return body
	}

	idx := -1
	var instructions string
	for i, raw := range inputArr {
		var item map[string]json.RawMessage
		if err := json.Unmarshal(raw, &item); err != nil {
			continue
		}
		var role string
		if err := json.Unmarshal(item["role"], &role); err != nil {
			continue
		}
		if role != "developer" && role != "system" {
			continue
		}
		text, ok := extractTextContent(item["content"])
		if !ok {
			continue
		}
		instructions = text
		idx = i
		break
	}
	if idx < 0 {
		return body
	}

	// Remove the hoisted item from input[]
	newInput := make([]json.RawMessage, 0, len(inputArr)-1)
	newInput = append(newInput, inputArr[:idx]...)
	newInput = append(newInput, inputArr[idx+1:]...)

	newInputBytes, err := json.Marshal(newInput)
	if err != nil {
		slog.Warn("codex transport: failed to re-marshal input", "err", err)
		return body
	}
	instructionsBytes, err := json.Marshal(instructions)
	if err != nil {
		slog.Warn("codex transport: failed to marshal instructions", "err", err)
		return body
	}
	m["input"] = json.RawMessage(newInputBytes)
	m["instructions"] = json.RawMessage(instructionsBytes)

	out, err := json.Marshal(m)
	if err != nil {
		slog.Warn("codex transport: failed to re-marshal hoisted body", "err", err)
		return body
	}
	return out
}

// extractTextContent pulls plain text from a Responses API content field, which
// can be either a JSON string or a list of {type:"input_text",text:"..."} parts.
// Returns false if the shape is neither.
func extractTextContent(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	// Try string form first.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, true
	}
	// Try list form.
	var parts []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &parts); err != nil {
		return "", false
	}
	var sb strings.Builder
	for _, p := range parts {
		var typ string
		if err := json.Unmarshal(p["type"], &typ); err != nil {
			continue
		}
		// Accept input_text and text both — Responses API uses input_text,
		// but defensively we accept "text" too in case fantasy emits it.
		if typ != "input_text" && typ != "text" {
			continue
		}
		var t string
		if err := json.Unmarshal(p["text"], &t); err != nil {
			continue
		}
		sb.WriteString(t)
	}
	if sb.Len() == 0 {
		return "", false
	}
	return sb.String(), true
}
