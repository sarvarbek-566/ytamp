// Package auth implements Google's OAuth device flow and token persistence.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/sarvarbek-566/ytamp/internal/config"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// Scope grants read access plus rating (like/unlike) and playlist edits.
const Scope = "https://www.googleapis.com/auth/youtube"

// ErrNoClient means client_id/secret aren't configured.
var ErrNoClient = errors.New("client_id sozlanmagan")

func tokenPath() string { return filepath.Join(config.StateDir(), "token.json") }

// OAuthConfig builds the oauth2 config from the user's credentials.
func OAuthConfig(c config.Config) (*oauth2.Config, error) {
	if c.ClientID == "" || c.ClientSecret == "" {
		return nil, ErrNoClient
	}
	return &oauth2.Config{
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		Endpoint:     google.Endpoint,
		Scopes:       []string{Scope},
	}, nil
}

// LoadToken reads the saved token, if any.
func LoadToken() (*oauth2.Token, error) {
	b, err := os.ReadFile(tokenPath())
	if err != nil {
		return nil, err
	}
	var t oauth2.Token
	return &t, json.Unmarshal(b, &t)
}

// SaveToken stores the token with 0600 permissions.
func SaveToken(t *oauth2.Token) error {
	if err := os.MkdirAll(config.StateDir(), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	tmp := tokenPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, tokenPath())
}

// Logout deletes the saved token.
func Logout() error {
	err := os.Remove(tokenPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Start begins the device flow and returns the code to show the user.
func Start(ctx context.Context, oc *oauth2.Config) (*oauth2.DeviceAuthResponse, error) {
	return oc.DeviceAuth(ctx)
}

// Wait polls until the user approves, then saves and returns the token.
func Wait(ctx context.Context, oc *oauth2.Config, da *oauth2.DeviceAuthResponse) (*oauth2.Token, error) {
	t, err := oc.DeviceAccessToken(ctx, da)
	if err != nil {
		return nil, err
	}
	return t, SaveToken(t)
}

// savingSource persists refreshed tokens so the refresh survives restarts.
type savingSource struct {
	mu   sync.Mutex
	src  oauth2.TokenSource
	last string
}

func (s *savingSource) Token() (*oauth2.Token, error) {
	t, err := s.src.Token()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if t.AccessToken != s.last {
		s.last = t.AccessToken
		_ = SaveToken(t)
	}
	return t, nil
}

// TokenSource wraps a token in an auto-refreshing, auto-saving source.
func TokenSource(ctx context.Context, oc *oauth2.Config, t *oauth2.Token) oauth2.TokenSource {
	return &savingSource{src: oc.TokenSource(ctx, t), last: t.AccessToken}
}
