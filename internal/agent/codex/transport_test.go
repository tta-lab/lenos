package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"charm.land/fantasy"
	"charm.land/fantasy/providers/openai"
)

func TestStripJSONField_RemovesMaxOutputTokens(t *testing.T) {
	body := []byte(`{"model":"gpt-5-codex","max_output_tokens":1000,"input":"hello"}`)
	stripped := stripJSONField(body, "max_output_tokens")
	if strings.Contains(string(stripped), "max_output_tokens") {
		t.Errorf("expected max_output_tokens to be removed, got: %s", string(stripped))
	}
	if !strings.Contains(string(stripped), "model") {
		t.Errorf("expected model field preserved, got: %s", string(stripped))
	}
	if !strings.Contains(string(stripped), "input") {
		t.Errorf("expected input field preserved, got: %s", string(stripped))
	}
}

func TestStripJSONField_PassThroughIfMissing(t *testing.T) {
	body := []byte(`{"model":"gpt-5-codex","input":"hello"}`)
	stripped := stripJSONField(body, "max_output_tokens")
	if string(stripped) != string(body) {
		t.Errorf("expected unchanged, got: %s", string(stripped))
	}
}

func TestStripJSONField_PassThroughIfMalformed(t *testing.T) {
	body := []byte(`not-json`)
	stripped := stripJSONField(body, "max_output_tokens")
	if string(stripped) != string(body) {
		t.Errorf("expected unchanged for non-JSON, got: %s", string(stripped))
	}
}

func TestStripJSONField_JSONArrayBody(t *testing.T) {
	body := []byte(`[{"model":"gpt-5-codex","max_output_tokens":1000}]`)
	stripped := stripJSONField(body, "max_output_tokens")
	if string(stripped) != string(body) {
		t.Errorf("expected array pass-through unchanged, got: %s", string(stripped))
	}
}

func TestStripJSONField_NestedFieldUntouched(t *testing.T) {
	body := []byte(`{"model":"gpt-5-codex","tools":[{"max_output_tokens":99}]}`)
	stripped := stripJSONField(body, "max_output_tokens")
	if string(stripped) != string(body) {
		t.Errorf("expected unchanged for nested-only field, got: %s", string(stripped))
	}
}

func TestStripTransport_PassThroughForNonJSON(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Write(body)
	}
	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()

	client := NewClient(nil)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL, strings.NewReader("hello world"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "text/plain")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	if string(got) != "hello world" {
		t.Errorf("expected passthrough, got: %s", string(got))
	}
}

// TestFantasyShape_DeveloperRoleStringContent is a regression test that
// captures the JSON body fantasy emits for a codex model (gpt-5.5) via the
// Responses API path and asserts key shape invariants. If a future fantasy
// version bump changes the emitted shape, this test will surface it.
func TestFantasyShape_DeveloperRoleStringContent(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"resp_1","object":"response","created_at":1,"model":"gpt-5.5","output":[],"usage":{"input_tokens":0,"output_tokens":0,"total_tokens":0,"input_tokens_details":{"cached_tokens":0},"output_tokens_details":{"reasoning_tokens":0}}}`))
	}))
	defer srv.Close()

	provider, err := openai.New(
		openai.WithAPIKey("test"),
		openai.WithBaseURL(srv.URL),
		openai.WithUseResponsesAPI(),
		openai.WithHTTPClient(srv.Client()),
	)
	if err != nil {
		t.Fatal(err)
	}
	model, err := provider.LanguageModel(context.Background(), "gpt-5.5")
	if err != nil {
		t.Fatal(err)
	}
	stream, err := model.Stream(context.Background(), fantasy.Call{
		Prompt: []fantasy.Message{
			fantasy.NewSystemMessage("you are kestrel"),
			fantasy.NewUserMessage("hi"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for range stream {
	}

	var got map[string]any
	if err := json.Unmarshal(captured, &got); err != nil {
		t.Fatalf("unmarshal captured body: %v\nbody: %s", err, string(captured))
	}
	// Invariant 1: no instructions field (the bug — set by hoist, not by fantasy)
	if _, ok := got["instructions"]; ok {
		t.Errorf("fantasy should NOT emit instructions; got instructions=%#v", got["instructions"])
	}
	// Invariant 2: input is a non-empty array
	input, ok := got["input"].([]any)
	if !ok {
		t.Fatalf("expected input to be array, got %T", got["input"])
	}
	if len(input) == 0 {
		t.Fatal("expected non-empty input array")
	}
	// Invariant 3: first input item is developer role with string content
	first, ok := input[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first input item to be object, got %T", input[0])
	}
	if first["role"] != "developer" {
		t.Errorf("expected first input role=developer, got %#v", first["role"])
	}
	if _, isStr := first["content"].(string); !isStr {
		t.Errorf("expected first input content to be string, got %T (%#v)", first["content"], first["content"])
	}
	// Invariant 4: stream:true at top level
	if s, ok := got["stream"].(bool); !ok || !s {
		t.Errorf("expected stream=true, got %#v", got["stream"])
	}
}

func TestHoistSystemToInstructions_DeveloperRoleStringContent(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","input":[{"role":"developer","content":"sys text"},{"role":"user","content":"hi"}],"store":false}`)
	out := hoistSystemToInstructions(body)

	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["instructions"] != "sys text" {
		t.Errorf("expected instructions=\"sys text\", got %#v", got["instructions"])
	}
	input, ok := got["input"].([]any)
	if !ok {
		t.Fatalf("expected input to be array, got %T", got["input"])
	}
	if len(input) != 1 {
		t.Errorf("expected developer item removed; input len=%d", len(input))
	}
	if first, _ := input[0].(map[string]any); first["role"] != "user" {
		t.Errorf("expected first remaining item role=user, got %#v", first["role"])
	}
}

func TestHoistSystemToInstructions_DeveloperRoleListContent(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","input":[{"role":"developer","content":[{"type":"input_text","text":"sys"}]},{"role":"user","content":"hi"}]}`)
	out := hoistSystemToInstructions(body)
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["instructions"] != "sys" {
		t.Errorf("expected instructions=\"sys\", got %#v", got["instructions"])
	}
	if input := got["input"].([]any); len(input) != 1 {
		t.Errorf("expected developer item removed; input len=%d", len(input))
	}
}

func TestHoistSystemToInstructions_SystemRole(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","input":[{"role":"system","content":"sys"},{"role":"user","content":"hi"}]}`)
	out := hoistSystemToInstructions(body)
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["instructions"] != "sys" {
		t.Errorf("expected hoist of system role; got instructions=%#v", got["instructions"])
	}
}

func TestHoistSystemToInstructions_NoSystemOrDeveloper_Passthrough(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","input":[{"role":"user","content":"hi"}]}`)
	out := hoistSystemToInstructions(body)
	if !bytes.Equal(out, body) {
		t.Errorf("expected passthrough; body changed:\nbefore: %s\nafter:  %s", body, out)
	}
}

func TestHoistSystemToInstructions_InstructionsAlreadySet_Passthrough(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","instructions":"existing","input":[{"role":"developer","content":"sys"}]}`)
	out := hoistSystemToInstructions(body)
	if !bytes.Equal(out, body) {
		t.Errorf("expected passthrough when instructions already set; body changed:\nbefore: %s\nafter:  %s", body, out)
	}
}

func TestHoistSystemToInstructions_MultipleDeveloperItems_OnlyFirstHoisted(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","input":[{"role":"developer","content":"first"},{"role":"developer","content":"second"},{"role":"user","content":"hi"}]}`)
	out := hoistSystemToInstructions(body)
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["instructions"] != "first" {
		t.Errorf("expected first hoisted; got %#v", got["instructions"])
	}
	input := got["input"].([]any)
	if len(input) != 2 {
		t.Fatalf("expected 2 remaining input items, got %d", len(input))
	}
	if first, _ := input[0].(map[string]any); first["role"] != "developer" || first["content"] != "second" {
		t.Errorf("expected second developer item to remain; got %#v", input[0])
	}
}

func TestHoistSystemToInstructions_InputAsString_Passthrough(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","input":"hello"}`)
	out := hoistSystemToInstructions(body)
	if !bytes.Equal(out, body) {
		t.Errorf("expected passthrough on string input; got %s", out)
	}
}

func TestHoistSystemToInstructions_InvalidJSON_Passthrough(t *testing.T) {
	body := []byte(`not json`)
	out := hoistSystemToInstructions(body)
	if !bytes.Equal(out, body) {
		t.Errorf("expected passthrough on invalid JSON; got %s", out)
	}
}

func TestHoistSystemToInstructions_NullContent_Passthrough(t *testing.T) {
	body := []byte(`{"input":[{"role":"developer","content":null},{"role":"user","content":"hi"}]}`)
	out := hoistSystemToInstructions(body)
	if !bytes.Equal(out, body) {
		t.Errorf("expected passthrough on null content; body changed:\nbefore: %s\nafter:  %s", body, out)
	}
}

func TestHoistSystemToInstructions_EmptyStringContent_Hoisted(t *testing.T) {
	body := []byte(`{"input":[{"role":"developer","content":""},{"role":"user","content":"hi"}]}`)
	out := hoistSystemToInstructions(body)
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["instructions"] != "" {
		t.Errorf("expected instructions=\"\", got %#v", got["instructions"])
	}
	input := got["input"].([]any)
	if len(input) != 1 {
		t.Errorf("expected developer item removed; input len=%d", len(input))
	}
}

func TestHoistSystemToInstructions_EmptyInputArray_Passthrough(t *testing.T) {
	body := []byte(`{"input":[],"model":"gpt-5.5"}`)
	out := hoistSystemToInstructions(body)
	if !bytes.Equal(out, body) {
		t.Errorf("expected passthrough on empty input array; body changed:\nbefore: %s\nafter:  %s", body, out)
	}
}

func TestHoistSystemToInstructions_ListContentTypeText_Hoisted(t *testing.T) {
	body := []byte(`{"input":[{"role":"developer","content":[{"type":"text","text":"sys"}]},{"role":"user","content":"hi"}]}`)
	out := hoistSystemToInstructions(body)
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["instructions"] != "sys" {
		t.Errorf("expected instructions=\"sys\", got %#v", got["instructions"])
	}
}

func TestHoistSystemToInstructions_NoInputField_Passthrough(t *testing.T) {
	body := []byte(`{"model":"gpt-5.5","store":false}`)
	out := hoistSystemToInstructions(body)
	if !bytes.Equal(out, body) {
		t.Errorf("expected passthrough on missing input; got %s", out)
	}
}

func TestStripTransport_RoundTrip_HoistsAndStrips(t *testing.T) {
	var receivedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := NewClient(srv.Client().Transport)

	body := `{"model":"gpt-5.5","max_output_tokens":1000,"input":[{"role":"developer","content":"sys"},{"role":"user","content":"hi"}],"store":false}`
	req, err := http.NewRequestWithContext(t.Context(), "POST", srv.URL, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	var got map[string]any
	if err := json.Unmarshal(receivedBody, &got); err != nil {
		t.Fatalf("unmarshal received: %v", err)
	}
	if _, present := got["max_output_tokens"]; present {
		t.Errorf("expected max_output_tokens stripped, still present")
	}
	if got["instructions"] != "sys" {
		t.Errorf("expected instructions=\"sys\", got %#v", got["instructions"])
	}
	input, _ := got["input"].([]any)
	if len(input) != 1 {
		t.Errorf("expected developer item removed; input len=%d", len(input))
	}
}

func TestStripTransport_EmptyBody(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}
	srv := httptest.NewServer(http.HandlerFunc(handler))
	defer srv.Close()

	tests := []struct {
		name string
		body io.Reader
	}{
		{"nil body", nil},
		{"NoBody", http.NoBody},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(nil)
			req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL, tt.body)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
		})
	}
}
