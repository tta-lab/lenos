package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	codexpkg "github.com/tta-lab/lenos/internal/agent/codex"
)

func TestInitiateDeviceAuth_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json, got %s", r.Header.Get("Content-Type"))
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["client_id"] != codexpkg.ClientID {
			t.Errorf("expected client_id %q, got %q", codexpkg.ClientID, body["client_id"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"device_auth_id":"abc123","user_code":"XYZ-123","interval":"5"}`)
	}))
	defer srv.Close()

	origURL := codexpkg.VarAuthURL
	codexpkg.VarAuthURL = srv.URL + "/usercode"
	defer func() { codexpkg.VarAuthURL = origURL }()

	dar, err := InitiateDeviceAuth(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if dar.DeviceAuthID != "abc123" {
		t.Errorf("expected DeviceAuthID abc123, got %q", dar.DeviceAuthID)
	}
	if dar.UserCode != "XYZ-123" {
		t.Errorf("expected UserCode XYZ-123, got %q", dar.UserCode)
	}
	if dar.Interval != 5 {
		t.Errorf("expected Interval 5, got %d", dar.Interval)
	}
	if dar.ExpiresIn != 300 {
		t.Errorf("expected ExpiresIn 300, got %d", dar.ExpiresIn)
	}
}

func TestInitiateDeviceAuth_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("oops"))
	}))
	defer srv.Close()

	origURL := codexpkg.VarAuthURL
	codexpkg.VarAuthURL = srv.URL + "/usercode"
	defer func() { codexpkg.VarAuthURL = origURL }()

	_, err := InitiateDeviceAuth(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestInitiateDeviceAuth_MissingFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	origURL := codexpkg.VarAuthURL
	codexpkg.VarAuthURL = srv.URL + "/usercode"
	defer func() { codexpkg.VarAuthURL = origURL }()

	_, err := InitiateDeviceAuth(context.Background())
	if err == nil {
		t.Fatal("expected error for missing fields")
	}
}

func TestPollForToken_Success(t *testing.T) {
	// Two servers: poll endpoint returns auth code, token endpoint returns OAuth tokens
	pollSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"authorization_code":"auth123","code_verifier":"verif456"}`)
	}))
	defer pollSrv.Close()

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"access_token":"test_access","refresh_token":"test_refresh","id_token":"test_id","expires_in":3600}`)
	}))
	defer tokenSrv.Close()

	origTokenURL := codexpkg.VarTokenURL
	codexpkg.VarTokenURL = tokenSrv.URL + "/token"
	defer func() { codexpkg.VarTokenURL = origTokenURL }()

	origAuthURL := codexpkg.VarAuthURL
	codexpkg.VarAuthURL = pollSrv.URL + "/deviceauth/usercode"
	defer func() { codexpkg.VarAuthURL = origAuthURL }()

	dar := &DeviceAuthResponse{
		DeviceAuthID: "dev123",
		UserCode:     "ABC-456",
		Interval:     0,
		ExpiresIn:    300,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tokens, err := PollForToken(ctx, dar)
	if err != nil {
		t.Fatal(err)
	}
	if tokens.AccessToken != "test_access" {
		t.Errorf("expected AccessToken test_access, got %q", tokens.AccessToken)
	}
	if tokens.RefreshToken != "test_refresh" {
		t.Errorf("expected RefreshToken test_refresh, got %q", tokens.RefreshToken)
	}
	if tokens.IDToken != "test_id" {
		t.Errorf("expected IDToken test_id, got %q", tokens.IDToken)
	}
	if tokens.ExpiresIn != 3600 {
		t.Errorf("expected ExpiresIn 3600, got %d", tokens.ExpiresIn)
	}
}

func TestPollForToken_PendingThenSuccess(t *testing.T) {
	var callCount int
	authSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		// First call to poll: 403 (pending), second call: success
		if callCount == 1 {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"authorization_code":"auth123","code_verifier":"verif456"}`)
	}))
	defer authSrv.Close()

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"access_token":"t","refresh_token":"r","id_token":"i","expires_in":3600}`)
	}))
	defer tokenSrv.Close()

	origAuthURL := codexpkg.VarAuthURL
	codexpkg.VarAuthURL = authSrv.URL + "/deviceauth/usercode"
	defer func() { codexpkg.VarAuthURL = origAuthURL }()

	origTokenURL := codexpkg.VarTokenURL
	codexpkg.VarTokenURL = tokenSrv.URL + "/token"
	defer func() { codexpkg.VarTokenURL = origTokenURL }()

	dar := &DeviceAuthResponse{
		DeviceAuthID: "dev123",
		UserCode:     "ABC-456",
		Interval:     0,
		ExpiresIn:    300,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tokens, err := PollForToken(ctx, dar)
	if err != nil {
		t.Fatal(err)
	}
	if tokens.AccessToken != "t" {
		t.Errorf("expected AccessToken t, got %q", tokens.AccessToken)
	}
}

func TestPollForToken_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	origAuthURL := codexpkg.VarAuthURL
	codexpkg.VarAuthURL = srv.URL + "/deviceauth/usercode"
	defer func() { codexpkg.VarAuthURL = origAuthURL }()

	origTokenURL := codexpkg.VarTokenURL
	codexpkg.VarTokenURL = srv.URL + "/token"
	defer func() { codexpkg.VarTokenURL = origTokenURL }()

	dar := &DeviceAuthResponse{
		DeviceAuthID: "dev123",
		UserCode:     "ABC-456",
		Interval:     0,
		ExpiresIn:    300,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := PollForToken(ctx, dar)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestPollForToken_NonPendingError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	origAuthURL := codexpkg.VarAuthURL
	codexpkg.VarAuthURL = srv.URL + "/deviceauth/usercode"
	defer func() { codexpkg.VarAuthURL = origAuthURL }()

	origTokenURL := codexpkg.VarTokenURL
	codexpkg.VarTokenURL = srv.URL + "/token"
	defer func() { codexpkg.VarTokenURL = origTokenURL }()

	dar := &DeviceAuthResponse{
		DeviceAuthID: "dev123",
		UserCode:     "ABC-456",
		Interval:     0,
		ExpiresIn:    300,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := PollForToken(ctx, dar)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRefreshToken_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Errorf("expected form-encoded, got %s", r.Header.Get("Content-Type"))
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("grant_type") != "refresh_token" {
			t.Errorf("expected grant_type refresh_token, got %q", r.Form.Get("grant_type"))
		}
		if r.Form.Get("refresh_token") != "my_refresh_token" {
			t.Errorf("expected refresh_token my_refresh_token, got %q", r.Form.Get("refresh_token"))
		}
		if r.Form.Get("client_id") != codexpkg.ClientID {
			t.Errorf("expected client_id %q, got %q", codexpkg.ClientID, r.Form.Get("client_id"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"access_token":"new_access","refresh_token":"new_refresh","expires_in":3600}`)
	}))
	defer srv.Close()

	origTokenURL := codexpkg.VarTokenURL
	codexpkg.VarTokenURL = srv.URL + "/token"
	defer func() { codexpkg.VarTokenURL = origTokenURL }()

	token, err := RefreshToken(context.Background(), "my_refresh_token")
	if err != nil {
		t.Fatal(err)
	}
	if token.AccessToken != "new_access" {
		t.Errorf("expected AccessToken new_access, got %q", token.AccessToken)
	}
	if token.RefreshToken != "new_refresh" {
		t.Errorf("expected RefreshToken new_refresh, got %q", token.RefreshToken)
	}
	if token.ExpiresIn != 3600 {
		t.Errorf("expected ExpiresIn 3600, got %d", token.ExpiresIn)
	}
}

func TestRefreshToken_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer srv.Close()

	origTokenURL := codexpkg.VarTokenURL
	codexpkg.VarTokenURL = srv.URL + "/token"
	defer func() { codexpkg.VarTokenURL = origTokenURL }()

	_, err := RefreshToken(context.Background(), "bad_token")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseInterval_String(t *testing.T) {
	if n := parseInterval("5"); n != 5 {
		t.Errorf("expected 5, got %d", n)
	}
	if n := parseInterval("0"); n != 5 {
		t.Errorf("expected 5 (clamped from 0), got %d", n)
	}
	if n := parseInterval("abc"); n != 5 {
		t.Errorf("expected 5 (fallback from abc), got %d", n)
	}
	if n := parseInterval(""); n != 5 {
		t.Errorf("expected 5 (empty), got %d", n)
	}
	if n := parseInterval("2"); n != 2 {
		t.Errorf("expected 2, got %d", n)
	}
}

func TestPollForToken_ContextDeadline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	origAuthURL := codexpkg.VarAuthURL
	codexpkg.VarAuthURL = srv.URL + "/deviceauth/usercode"
	defer func() { codexpkg.VarAuthURL = origAuthURL }()

	origTokenURL := codexpkg.VarTokenURL
	codexpkg.VarTokenURL = srv.URL + "/token"
	defer func() { codexpkg.VarTokenURL = origTokenURL }()

	dar := &DeviceAuthResponse{
		DeviceAuthID: "dev123",
		UserCode:     "ABC-456",
		Interval:     0,
		ExpiresIn:    300,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := PollForToken(ctx, dar)
	if err == nil {
		t.Fatal("expected error due to context deadline")
	}
}
