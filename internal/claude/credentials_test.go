package claude

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/leeaandrob/claudex/internal/observability"
)

// writeCredsFile writes a credentials.json with the given oauth body into a
// temp dir and returns the file path.
func writeCredsFile(t *testing.T, oauth map[string]any) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".credentials.json")
	data, err := json.Marshal(map[string]any{"claudeAiOauth": oauth})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func newTestManager(t *testing.T, path, tokenURL string) *CredentialManager {
	t.Helper()
	return &CredentialManager{
		path:      path,
		client:    &http.Client{Timeout: 5 * time.Second},
		tokenURL:  tokenURL,
		clientID:  "test-client",
		userAgent: "test-agent",
		logger:    observability.NewLogger("debug"),
	}
}

func readCreds(t *testing.T, path string) oauthCredentials {
	t.Helper()
	m := newTestManager(t, path, "")
	creds, err := m.load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return *creds
}

func TestEnsureFresh_NoFile(t *testing.T) {
	m := newTestManager(t, filepath.Join(t.TempDir(), "missing.json"), "")
	if err := m.EnsureFresh(context.Background()); err != nil {
		t.Fatalf("expected nil for missing file, got %v", err)
	}
}

func TestEnsureFresh_ValidTokenNoRefresh(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
	}))
	defer srv.Close()

	// Expires far in the future → no refresh.
	path := writeCredsFile(t, map[string]any{
		"accessToken":  "old-access",
		"refreshToken": "old-refresh",
		"expiresAt":    time.Now().Add(2 * time.Hour).UnixMilli(),
	})
	m := newTestManager(t, path, srv.URL)

	if err := m.EnsureFresh(context.Background()); err != nil {
		t.Fatalf("EnsureFresh: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("expected no refresh calls, got %d", got)
	}
}

func TestEnsureFresh_NearExpiryRefreshes(t *testing.T) {
	var gotBody map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ua := r.Header.Get("User-Agent"); ua != "test-agent" {
			t.Errorf("User-Agent = %q, want test-agent", ua)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(oauthTokenResponse{
			AccessToken:  "new-access",
			RefreshToken: "new-refresh",
			ExpiresIn:    28800,
			TokenType:    "Bearer",
		})
	}))
	defer srv.Close()

	// Expired token with extra unknown fields that must be preserved.
	path := writeCredsFile(t, map[string]any{
		"accessToken":      "old-access",
		"refreshToken":     "old-refresh",
		"expiresAt":        time.Now().Add(-time.Minute).UnixMilli(),
		"subscriptionType": "max",
		"scopes":           []string{"user:inference"},
	})
	m := newTestManager(t, path, srv.URL)

	if err := m.EnsureFresh(context.Background()); err != nil {
		t.Fatalf("EnsureFresh: %v", err)
	}

	if gotBody["grant_type"] != "refresh_token" || gotBody["refresh_token"] != "old-refresh" || gotBody["client_id"] != "test-client" {
		t.Fatalf("unexpected refresh body: %#v", gotBody)
	}

	creds := readCreds(t, path)
	if creds.AccessToken != "new-access" {
		t.Errorf("accessToken = %q, want new-access", creds.AccessToken)
	}
	if creds.RefreshToken != "new-refresh" {
		t.Errorf("refreshToken = %q, want new-refresh (rotated)", creds.RefreshToken)
	}
	if creds.expiringWithin(time.Minute) {
		t.Errorf("expiresAt not advanced: %d", creds.ExpiresAt)
	}

	// Unknown field must survive the round-trip.
	if v, ok := creds.raw["subscriptionType"]; !ok || string(v) != `"max"` {
		t.Errorf("subscriptionType not preserved: %s ok=%v", v, ok)
	}
}

func TestEnsureFresh_NoRefreshToken(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
	}))
	defer srv.Close()

	path := writeCredsFile(t, map[string]any{
		"accessToken": "only-access",
		"expiresAt":   time.Now().Add(-time.Hour).UnixMilli(),
	})
	m := newTestManager(t, path, srv.URL)

	if err := m.EnsureFresh(context.Background()); err != nil {
		t.Fatalf("EnsureFresh: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("expected no refresh without refresh token, got %d calls", got)
	}
}

func TestForceRefresh_AlwaysRefreshes(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_ = json.NewEncoder(w).Encode(oauthTokenResponse{AccessToken: "fresh", ExpiresIn: 28800})
	}))
	defer srv.Close()

	// Token valid for hours; ForceRefresh should still hit the endpoint.
	path := writeCredsFile(t, map[string]any{
		"accessToken":  "old",
		"refreshToken": "rt",
		"expiresAt":    time.Now().Add(5 * time.Hour).UnixMilli(),
	})
	m := newTestManager(t, path, srv.URL)

	if err := m.ForceRefresh(context.Background()); err != nil {
		t.Fatalf("ForceRefresh: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected 1 refresh call, got %d", got)
	}
	if creds := readCreds(t, path); creds.AccessToken != "fresh" {
		t.Errorf("accessToken = %q, want fresh", creds.AccessToken)
	}
}

func TestEnsureFresh_NotWritableSkipsRefresh(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_ = json.NewEncoder(w).Encode(oauthTokenResponse{AccessToken: "new", RefreshToken: "rotated", ExpiresIn: 28800})
	}))
	defer srv.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, ".credentials.json")
	data, _ := json.Marshal(map[string]any{"claudeAiOauth": map[string]any{
		"accessToken":  "old-access",
		"refreshToken": "old-refresh",
		"expiresAt":    time.Now().Add(-time.Minute).UnixMilli(),
	}})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Make the directory read-only so probe/temp creation fails.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	m := newTestManager(t, path, srv.URL)
	err := m.EnsureFresh(context.Background())
	if err == nil {
		t.Fatal("expected an error when the directory is not writable")
	}
	// Critically: the refresh endpoint must NOT have been called, so the
	// existing refresh token is preserved (not rotated and lost).
	if got := atomic.LoadInt32(&calls); got != 0 {
		t.Fatalf("expected no refresh call when unable to persist, got %d", got)
	}
	_ = os.Chmod(dir, 0o700)
	if creds := readCreds(t, path); creds.RefreshToken != "old-refresh" {
		t.Fatalf("refresh token must be preserved, got %q", creds.RefreshToken)
	}
}

func TestIsAuthError(t *testing.T) {
	cases := map[string]bool{
		`claude cli error: exit status 1: {"api_error_status":401}`: true,
		"API Error: 401 Invalid authentication credentials":         true,
		"authentication_error: token expired":                       true,
		"some unrelated failure":                                    false,
	}
	for in, want := range cases {
		if got := isAuthError(in); got != want {
			t.Errorf("isAuthError(%q) = %v, want %v", in, got, want)
		}
	}
}
