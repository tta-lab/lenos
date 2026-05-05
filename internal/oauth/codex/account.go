package codex

import (
	"encoding/base64"
	"encoding/json"
	"regexp"
	"strings"
)

// safeAccountIDRegex validates that a chatgpt_account_id contains only
// alphanumeric characters, underscores, and hyphens. This prevents CRLF
// injection into HTTP headers (the account ID is used in ExtraHeaders).
var safeAccountIDRegex = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// ExtractChatGPTAccountID parses a JWT and returns the chatgpt_account_id claim.
// Tries (in order):
//  1. claims["chatgpt_account_id"]
//  2. claims["https://api.openai.com/auth"]["chatgpt_account_id"]
//  3. claims["organizations"][0]["id"]
//
// Returns "" if none found or if the value contains unsafe characters.
//
// The JWT signature is NOT verified — we trust it because we just received it
// over TLS from auth.openai.com via PKCE-equivalent code exchange.
func ExtractChatGPTAccountID(token string) string {
	if token == "" {
		return ""
	}

	// Split JWT into parts: header.payload.signature
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return ""
	}

	// Decode the payload (middle segment)
	payload, err := decodeJWTSegment(parts[1])
	if err != nil {
		return ""
	}

	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return ""
	}

	// Strategy 1: direct chatgpt_account_id claim
	if v, ok := getString(claims, "chatgpt_account_id"); ok {
		return validateAccountID(v)
	}

	// Strategy 2: nested https://api.openai.com/auth.chatgpt_account_id
	if auth, ok := claims["https://api.openai.com/auth"].(map[string]any); ok {
		if v, ok := getString(auth, "chatgpt_account_id"); ok {
			return validateAccountID(v)
		}
	}

	// Strategy 3: first organization ID
	if orgs, ok := claims["organizations"].([]any); ok && len(orgs) > 0 {
		if org, ok := orgs[0].(map[string]any); ok {
			if v, ok := getString(org, "id"); ok {
				return validateAccountID(v)
			}
		}
	}

	return ""
}

// getString extracts a string value from a map, returning whether the key exists and is a string.
func getString(m map[string]any, key string) (string, bool) {
	v, ok := m[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// validateAccountID checks the account ID against the safe regex.
// Returns the ID if valid, empty string otherwise.
func validateAccountID(id string) string {
	if safeAccountIDRegex.MatchString(id) {
		return id
	}
	return ""
}

// decodeJWTSegment decodes a base64-encoded JWT segment (header or payload).
// Handles both RawURLEncoding and URLEncoding (with padding stripped).
func decodeJWTSegment(seg string) ([]byte, error) {
	// Restore padding if needed
	switch len(seg) % 4 {
	case 2:
		seg += "=="
	case 3:
		seg += "="
	}

	decoded, err := base64.URLEncoding.DecodeString(seg)
	if err == nil {
		return decoded, nil
	}

	// Try RawURLEncoding as fallback
	return base64.RawURLEncoding.DecodeString(seg)
}
