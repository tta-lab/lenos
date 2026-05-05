// Package codex provides the Codex (ChatGPT consumer OAuth) device-auth flow.
package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	codexpkg "github.com/tta-lab/lenos/internal/agent/codex"
	"github.com/tta-lab/lenos/internal/oauth"
)

// DeviceAuthResponse is the parsed response from the usercode endpoint.
type DeviceAuthResponse struct {
	DeviceAuthID string // returned in `device_auth_id` field
	UserCode     string // returned in `user_code` field
	Interval     int    // returned as a string in `interval` field; parse to int, default 5, min 1
	VerifyURL    string // = codexpkg.VerifyURL
	ExpiresIn    int    // hardcode 300 (5 min) — Codex's usercode response doesn't include expires_in
}

// usercodeResponse is the raw JSON response from the /usercode endpoint.
type usercodeResponse struct {
	DeviceAuthID string `json:"device_auth_id"`
	UserCode     string `json:"user_code"`
	Interval     string `json:"interval"`
}

// pollResponse is the raw JSON response from the poll endpoint.
type pollResponse struct {
	AuthorizationCode string `json:"authorization_code"`
	CodeVerifier      string `json:"code_verifier"`
}

// tokenResponse is the raw JSON response from /oauth/token.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
}

// TokenSet bundles the OAuth response with the id_token.
type TokenSet struct {
	*oauth.Token
	IDToken string
}

// InitiateDeviceAuth — POST AuthURL with `{"client_id": ClientID}` (Content-Type: application/json).
// Returns parsed DeviceAuthResponse. Use http.Client with 30s timeout.
func InitiateDeviceAuth(ctx context.Context) (*DeviceAuthResponse, error) {
	body := map[string]string{"client_id": codexpkg.ClientID}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, codexpkg.VarAuthURL, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("usercode request failed: status %d body %q", resp.StatusCode, string(respBody))
	}

	var ur usercodeResponse
	if err := json.Unmarshal(respBody, &ur); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if ur.DeviceAuthID == "" || ur.UserCode == "" {
		return nil, fmt.Errorf("usercode response missing required fields: %s", string(respBody))
	}

	interval := parseInterval(ur.Interval)

	return &DeviceAuthResponse{
		DeviceAuthID: ur.DeviceAuthID,
		UserCode:     ur.UserCode,
		Interval:     interval,
		VerifyURL:    codexpkg.VerifyURL,
		ExpiresIn:    300,
	}, nil
}

// parseInterval parses the interval string from the usercode response.
// Returns default 5 if parsing fails, clamped to min 1.
func parseInterval(s string) int {
	if s == "" {
		return 5
	}
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil || n <= 0 {
		return 5
	}
	return n
}

// PollForToken — polls the derived poll_url every `interval+3` seconds.
// poll_url = strings.Replace(VarAuthURL, "/usercode", "/token", 1).
// 200 success → parse {authorization_code, code_verifier} → immediately exchange for OAuth tokens.
// 403 / 404 → continue (auth pending).
// Other status → terminal error.
// Returns the OAuth TokenSet from Step 3.
func PollForToken(ctx context.Context, dar *DeviceAuthResponse) (*TokenSet, error) {
	pollURL := strings.Replace(codexpkg.VarAuthURL, "/usercode", "/token", 1)
	wait := time.Duration(dar.Interval+3) * time.Second
	deadline := time.Now().Add(time.Duration(dar.ExpiresIn) * time.Second)

	ticker := time.NewTicker(wait)
	defer ticker.Stop()

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}

		authCode, codeVerifier, err := pollOnce(ctx, pollURL, dar.DeviceAuthID, dar.UserCode)
		if err != nil {
			if errors.Is(err, errPending) {
				continue
			}
			return nil, err
		}

		// Step 3: exchange auth code for tokens
		tokens, err := exchangeAuthCode(ctx, authCode, codeVerifier)
		if err != nil {
			return nil, fmt.Errorf("exchange auth code: %w", err)
		}
		return tokens, nil
	}

	return nil, fmt.Errorf("authorization timed out")
}

var errPending = fmt.Errorf("pending")

// pollOnce performs one poll request to the poll endpoint.
// Returns (authorization_code, code_verifier, nil) on success.
// Returns ("", "", errPending) if auth is still pending.
func pollOnce(ctx context.Context, pollURL, deviceAuthID, userCode string) (string, string, error) {
	body := map[string]string{
		"device_auth_id": deviceAuthID,
		"user_code":      userCode,
	}
	data, err := json.Marshal(body)
	if err != nil {
		return "", "", fmt.Errorf("marshal poll request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, pollURL, bytes.NewReader(data))
	if err != nil {
		return "", "", fmt.Errorf("create poll request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("execute poll request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", "", fmt.Errorf("read poll response: %w", err)
	}

	// 403 or 404 = auth pending
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
		slog.Debug("codex poll: auth pending", "status", resp.StatusCode, "body", string(respBody))
		return "", "", errPending
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("poll request failed: status %d body %q", resp.StatusCode, string(respBody))
	}

	var pr pollResponse
	if err := json.Unmarshal(respBody, &pr); err != nil {
		return "", "", fmt.Errorf("unmarshal poll response: %w", err)
	}
	if pr.AuthorizationCode == "" || pr.CodeVerifier == "" {
		return "", "", fmt.Errorf("poll response missing required fields: %s", string(respBody))
	}

	return pr.AuthorizationCode, pr.CodeVerifier, nil
}

// exchangeAuthCode exchanges an authorization code for OAuth tokens via the standard
// /oauth/token endpoint (form-encoded POST).
func exchangeAuthCode(ctx context.Context, authCode, codeVerifier string) (*TokenSet, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", authCode)
	form.Set("redirect_uri", "https://auth.openai.com/deviceauth/callback")
	form.Set("client_id", codexpkg.ClientID)
	form.Set("code_verifier", codeVerifier)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, codexpkg.VarTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute token request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed: status %d body %q", resp.StatusCode, string(respBody))
	}

	var tr tokenResponse
	if err := json.Unmarshal(respBody, &tr); err != nil {
		return nil, fmt.Errorf("unmarshal token response: %w", err)
	}
	if tr.AccessToken == "" {
		return nil, fmt.Errorf("token response missing access_token: %s", string(respBody))
	}

	token := &oauth.Token{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		ExpiresIn:    tr.ExpiresIn,
	}
	token.SetExpiresAt()

	return &TokenSet{
		Token:   token,
		IDToken: tr.IDToken,
	}, nil
}

// RefreshToken — standard OAuth refresh. POST TokenURL with form-encoded
// `grant_type=refresh_token, refresh_token=<...>, client_id=ClientID`.
func RefreshToken(ctx context.Context, refreshToken string) (*oauth.Token, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", codexpkg.ClientID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, codexpkg.VarTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute refresh request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read refresh response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token refresh failed: status %d body %q", resp.StatusCode, string(respBody))
	}

	var tr tokenResponse
	if err := json.Unmarshal(respBody, &tr); err != nil {
		return nil, fmt.Errorf("unmarshal refresh response: %w", err)
	}
	if tr.AccessToken == "" {
		return nil, fmt.Errorf("refresh response missing access_token: %s", string(respBody))
	}

	token := &oauth.Token{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		ExpiresIn:    tr.ExpiresIn,
	}
	token.SetExpiresAt()

	return token, nil
}
