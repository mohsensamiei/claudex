package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/leeaandrob/claudex/internal/observability"
)

// Claude Code OAuth constants. These mirror the values the Claude CLI itself
// uses to refresh subscription (claude.ai) credentials. They can be overridden
// via environment variables in case the upstream endpoint/client changes or a
// different User-Agent is needed to get past bot/WAF filtering on headless
// hosts (a known issue for OAuth refresh from Linux servers).
const (
	defaultOAuthTokenURL  = "https://platform.claude.com/v1/oauth/token"
	defaultOAuthClientID  = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	defaultOAuthUserAgent = "claude-cli/1.0 (external, cli)"

	// refreshBuffer is how long before the recorded expiry we proactively
	// refresh. The CLI refreshes ~5 minutes early; we use the same window.
	refreshBuffer = 5 * time.Minute
)

// credentialsFile is the on-disk shape of ~/.claude/.credentials.json.
type credentialsFile struct {
	ClaudeAIOauth oauthCredentials `json:"claudeAiOauth"`
}

// oauthCredentials holds the OAuth token material the CLI persists. Unknown
// fields (subscriptionType, rateLimitTier, ...) are preserved via raw so a
// refresh never drops metadata the CLI relies on.
type oauthCredentials struct {
	AccessToken  string   `json:"accessToken"`
	RefreshToken string   `json:"refreshToken"`
	ExpiresAt    int64    `json:"expiresAt"` // unix milliseconds
	Scopes       []string `json:"scopes,omitempty"`

	// raw captures every field present in the file so we can round-trip
	// unknown keys untouched.
	raw map[string]json.RawMessage
}

// oauthTokenResponse is the JSON returned by the OAuth token endpoint.
type oauthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"` // seconds
	TokenType    string `json:"token_type"`
}

// CredentialManager keeps the Claude CLI's OAuth credentials fresh.
//
// The Claude CLI is supposed to refresh its own access token using the stored
// refresh token, but in a headless container (claude -p) that refresh often
// does not happen, so the access token expires after a few hours and every
// request then fails with a 401. This manager performs the refresh itself
// before invoking the CLI: it reads .credentials.json, and if the access token
// is at/near expiry it exchanges the refresh token for a new access token and
// writes the rotated credentials back to disk for the CLI to use.
type CredentialManager struct {
	path      string
	client    *http.Client
	tokenURL  string
	clientID  string
	userAgent string
	logger    *observability.Logger

	mu sync.Mutex
}

// NewCredentialManager builds a manager for the standard credentials path
// (~/.claude/.credentials.json), honoring overrides via environment.
func NewCredentialManager(logger *observability.Logger) *CredentialManager {
	path := os.Getenv("CLAUDEX_CREDENTIALS_PATH")
	if path == "" {
		home := os.Getenv("HOME")
		if home == "" {
			home, _ = os.UserHomeDir()
		}
		path = filepath.Join(home, ".claude", ".credentials.json")
	}

	return &CredentialManager{
		path:      path,
		client:    &http.Client{Timeout: 30 * time.Second},
		tokenURL:  envOr("CLAUDEX_OAUTH_TOKEN_URL", defaultOAuthTokenURL),
		clientID:  envOr("CLAUDEX_OAUTH_CLIENT_ID", defaultOAuthClientID),
		userAgent: envOr("CLAUDEX_OAUTH_USER_AGENT", defaultOAuthUserAgent),
		logger:    logger,
	}
}

// EnsureFresh refreshes the access token if it is missing or within
// refreshBuffer of expiry. It is safe for concurrent use. Absence of a
// credentials file or a refresh token is not an error: the manager simply does
// nothing and lets the CLI use whatever auth it has (e.g. an API key or a
// long-lived token).
func (m *CredentialManager) EnsureFresh(ctx context.Context) error {
	return m.refreshIfNeeded(ctx, false)
}

// ForceRefresh refreshes regardless of the recorded expiry. Use it reactively
// after the CLI reports a 401, in case the stored expiry was stale.
func (m *CredentialManager) ForceRefresh(ctx context.Context) error {
	return m.refreshIfNeeded(ctx, true)
}

func (m *CredentialManager) refreshIfNeeded(ctx context.Context, force bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.logger.Debug("credential check starting", "path", m.path, "forced", force)

	creds, err := m.load()
	if err != nil {
		if os.IsNotExist(err) {
			m.logger.Debug("credential refresh skipped: no credentials file", "path", m.path)
			return nil // no file: nothing to manage
		}
		m.logger.Error("credential load failed", "path", m.path, "error", err.Error())
		return err
	}

	// Nothing to refresh with.
	if creds.RefreshToken == "" {
		m.logger.Debug("credential refresh skipped: no refresh token present", "path", m.path)
		return nil
	}

	expiry := time.UnixMilli(creds.ExpiresAt)
	remaining := time.Until(expiry).Round(time.Second)

	if !force && !creds.expiringWithin(refreshBuffer) {
		m.logger.Debug("credential still valid, no refresh needed",
			"expires_at", expiry.Format(time.RFC3339),
			"expires_in", remaining.String())
		return nil // still valid
	}

	m.logger.Info("refreshing OAuth access token",
		"reason", refreshReason(force, creds),
		"expires_at", expiry.Format(time.RFC3339),
		"expires_in", remaining.String(),
		"endpoint", m.tokenURL)

	resp, err := m.requestRefresh(ctx, creds.RefreshToken)
	if err != nil {
		m.logger.Error("OAuth token refresh request failed", "endpoint", m.tokenURL, "error", err.Error())
		return err
	}

	rotated := resp.RefreshToken != ""
	creds.applyTokenResponse(resp)

	if err := m.save(creds); err != nil {
		m.logger.Error("failed to persist refreshed credentials", "path", m.path, "error", err.Error())
		return err
	}

	newExpiry := time.UnixMilli(creds.ExpiresAt)
	m.logger.Info("OAuth access token refreshed",
		"new_expires_at", newExpiry.Format(time.RFC3339),
		"valid_for", time.Until(newExpiry).Round(time.Second).String(),
		"refresh_token_rotated", rotated,
		"path", m.path)
	return nil
}

// refreshReason describes why a refresh is being performed, for logging.
func refreshReason(force bool, c *oauthCredentials) string {
	switch {
	case force:
		return "forced (reactive 401 retry)"
	case c.ExpiresAt == 0:
		return "unknown expiry"
	default:
		return "at or near expiry"
	}
}

// expiringWithin reports whether the token expires within d from now (or is
// already expired). ExpiresAt is unix milliseconds.
func (c *oauthCredentials) expiringWithin(d time.Duration) bool {
	if c.ExpiresAt == 0 {
		return true // unknown expiry: treat as needing refresh
	}
	expiry := time.UnixMilli(c.ExpiresAt)
	return time.Now().Add(d).After(expiry)
}

// applyTokenResponse updates the credential fields from a refresh response,
// preserving the rotated refresh token when the endpoint returns one.
func (c *oauthCredentials) applyTokenResponse(r *oauthTokenResponse) {
	c.AccessToken = r.AccessToken
	if r.RefreshToken != "" {
		c.RefreshToken = r.RefreshToken
	}
	if r.ExpiresIn > 0 {
		c.ExpiresAt = time.Now().Add(time.Duration(r.ExpiresIn) * time.Second).UnixMilli()
	}
}

func (m *CredentialManager) requestRefresh(ctx context.Context, refreshToken string) (*oauthTokenResponse, error) {
	body, err := json.Marshal(map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
		"client_id":     m.clientID,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal refresh request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.tokenURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", m.userAgent)

	m.logger.Debug("posting OAuth refresh request", "endpoint", m.tokenURL, "user_agent", m.userAgent)

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oauth refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return nil, fmt.Errorf("read refresh response: %w", err)
	}

	// Status only — the success body carries fresh tokens and must not be logged.
	m.logger.Debug("OAuth refresh response received", "status", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oauth refresh returned %d: %s", resp.StatusCode, buf.String())
	}

	var tok oauthTokenResponse
	if err := json.Unmarshal(buf.Bytes(), &tok); err != nil {
		return nil, fmt.Errorf("decode refresh response: %w", err)
	}
	if tok.AccessToken == "" {
		return nil, fmt.Errorf("oauth refresh response missing access_token")
	}
	return &tok, nil
}

// load reads and parses the credentials file, preserving unknown fields.
func (m *CredentialManager) load() (*oauthCredentials, error) {
	data, err := os.ReadFile(m.path)
	if err != nil {
		return nil, err
	}

	var file struct {
		ClaudeAIOauth map[string]json.RawMessage `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse credentials file: %w", err)
	}

	creds := &oauthCredentials{raw: file.ClaudeAIOauth}
	if creds.raw == nil {
		creds.raw = map[string]json.RawMessage{}
	}
	unmarshalRaw(creds.raw, "accessToken", &creds.AccessToken)
	unmarshalRaw(creds.raw, "refreshToken", &creds.RefreshToken)
	unmarshalRaw(creds.raw, "expiresAt", &creds.ExpiresAt)
	unmarshalRaw(creds.raw, "scopes", &creds.Scopes)
	return creds, nil
}

// save writes the credentials back atomically with 0600 permissions, keeping
// any unknown fields that were present in the original file.
func (m *CredentialManager) save(creds *oauthCredentials) error {
	setRaw(creds.raw, "accessToken", creds.AccessToken)
	setRaw(creds.raw, "refreshToken", creds.RefreshToken)
	setRaw(creds.raw, "expiresAt", creds.ExpiresAt)

	out := map[string]any{"claudeAiOauth": creds.raw}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal credentials: %w", err)
	}

	dir := filepath.Dir(m.path)
	tmp, err := os.CreateTemp(dir, ".credentials-*.json")
	if err != nil {
		return fmt.Errorf("create temp credentials file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once renamed

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp credentials: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod temp credentials: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp credentials: %w", err)
	}
	if err := os.Rename(tmpName, m.path); err != nil {
		return fmt.Errorf("replace credentials file: %w", err)
	}
	return nil
}

func unmarshalRaw[T any](raw map[string]json.RawMessage, key string, dst *T) {
	if v, ok := raw[key]; ok {
		_ = json.Unmarshal(v, dst)
	}
}

func setRaw(raw map[string]json.RawMessage, key string, value any) {
	if b, err := json.Marshal(value); err == nil {
		raw[key] = b
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
