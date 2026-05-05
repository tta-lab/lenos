package codex

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"
)

// buildJWT builds a mock JWT with the given claims as the payload.
func buildJWT(claims map[string]any) string {
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
	payload, _ := json.Marshal(claims)
	h := base64.RawURLEncoding.EncodeToString(header)
	p := base64.RawURLEncoding.EncodeToString(payload)
	return fmt.Sprintf("%s.%s.fakesignature", h, p)
}

func TestExtractChatGPTAccountID_DirectClaim(t *testing.T) {
	jwt := buildJWT(map[string]any{
		"chatgpt_account_id": "user_abc123",
		"sub":                "test",
	})
	id := ExtractChatGPTAccountID(jwt)
	if id != "user_abc123" {
		t.Errorf("expected user_abc123, got %q", id)
	}
}

func TestExtractChatGPTAccountID_NestedClaim(t *testing.T) {
	jwt := buildJWT(map[string]any{
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": "nested_user",
		},
	})
	id := ExtractChatGPTAccountID(jwt)
	if id != "nested_user" {
		t.Errorf("expected nested_user, got %q", id)
	}
}

func TestExtractChatGPTAccountID_OrganizationID(t *testing.T) {
	jwt := buildJWT(map[string]any{
		"organizations": []any{
			map[string]any{"id": "org_789"},
		},
	})
	id := ExtractChatGPTAccountID(jwt)
	if id != "org_789" {
		t.Errorf("expected org_789, got %q", id)
	}
}

func TestExtractChatGPTAccountID_MissingClaim(t *testing.T) {
	jwt := buildJWT(map[string]any{
		"sub": "test",
	})
	id := ExtractChatGPTAccountID(jwt)
	if id != "" {
		t.Errorf("expected empty, got %q", id)
	}
}

func TestExtractChatGPTAccountID_MalformedJWT(t *testing.T) {
	// No dots
	id := ExtractChatGPTAccountID("notajwt")
	if id != "" {
		t.Errorf("expected empty, got %q", id)
	}

	// Empty token
	id = ExtractChatGPTAccountID("")
	if id != "" {
		t.Errorf("expected empty, got %q", id)
	}

	// Invalid base64
	id = ExtractChatGPTAccountID("header.!!!.sig")
	if id != "" {
		t.Errorf("expected empty, got %q", id)
	}
}

func TestExtractChatGPTAccountID_RejectsControlChars(t *testing.T) {
	// Claim with CRLF injection attempt
	jwt := buildJWT(map[string]any{
		"chatgpt_account_id": "abc\r\nX-Injected: yes",
	})
	id := ExtractChatGPTAccountID(jwt)
	if id != "" {
		t.Errorf("expected empty for CRLF injection, got %q", id)
	}
}

func TestExtractChatGPTAccountID_RejectsNonAlphanumeric(t *testing.T) {
	// Claim with script injection
	jwt := buildJWT(map[string]any{
		"chatgpt_account_id": "abc<script>",
	})
	id := ExtractChatGPTAccountID(jwt)
	if id != "" {
		t.Errorf("expected empty for script injection, got %q", id)
	}
}

func TestExtractChatGPTAccountID_AcceptsValidPatterns(t *testing.T) {
	tests := []struct {
		name  string
		claim string
	}{
		{"alphanumeric", "user123ABC"},
		{"with hyphens", "user-abc-123"},
		{"with underscores", "user_abc_123"},
		{"mixed", "user_ABC-123"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jwt := buildJWT(map[string]any{
				"chatgpt_account_id": tt.claim,
			})
			id := ExtractChatGPTAccountID(jwt)
			if id != tt.claim {
				t.Errorf("expected %q, got %q", tt.claim, id)
			}
		})
	}
}

func TestDecodeJWTSegment_URLEncoding(t *testing.T) {
	// Standard URLEncoding with padding
	payload := `{"sub":"test"}`
	encoded := base64.URLEncoding.EncodeToString([]byte(payload))
	decoded, err := decodeJWTSegment(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != payload {
		t.Errorf("expected %q, got %q", payload, string(decoded))
	}
}

func TestDecodeJWTSegment_RawURLEncoding(t *testing.T) {
	// Raw URL without padding
	payload := `{"sub":"test"}`
	encoded := base64.RawURLEncoding.EncodeToString([]byte(payload))
	decoded, err := decodeJWTSegment(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != payload {
		t.Errorf("expected %q, got %q", payload, string(decoded))
	}
}
