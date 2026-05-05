package codex

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
	resp, err := client.Post(srv.URL, "text/plain", strings.NewReader("hello world"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	if string(got) != "hello world" {
		t.Errorf("expected passthrough, got: %s", string(got))
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
			req, err := http.NewRequest("POST", srv.URL, tt.body)
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
