package ytdl

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sarvarbek-566/ytamp/internal/track"
	"github.com/sarvarbek-566/ytamp/internal/youtube"
)

// maxLibrary caps how many tracks are read from one list.
const maxLibrary = 1000

// Browser is a browser yt-dlp can borrow a YouTube session from.
type Browser struct {
	Name  string // yt-dlp name: firefox, chrome, …
	Label string
	Path  string // profile root found on disk
}

// CookieArg is the --cookies-from-browser value for a browser name. It pins
// the profile directory we found, because yt-dlp's own lookup misses
// profiles when XDG_CONFIG_HOME is non-default (or for snap/flatpak).
func CookieArg(name string) string {
	if name == "" || strings.Contains(name, ":") {
		return name
	}
	for _, b := range DetectBrowsers() {
		if b.Name == name {
			return name + ":" + b.Path
		}
	}
	return name
}

// DetectBrowsers lists browsers that have a profile on this machine.
func DetectBrowsers() []Browser {
	h, _ := os.UserHomeDir()
	cands := []struct {
		b     Browser
		paths []string
	}{
		{Browser{Name: "firefox", Label: "Firefox"}, []string{".config/mozilla/firefox", ".mozilla/firefox",
			".var/app/org.mozilla.firefox/config/mozilla/firefox", ".var/app/org.mozilla.firefox/.mozilla/firefox",
			"snap/firefox/common/.mozilla/firefox"}},
		{Browser{Name: "chrome", Label: "Google Chrome"}, []string{".config/google-chrome", ".var/app/com.google.Chrome/config/google-chrome"}},
		{Browser{Name: "chromium", Label: "Chromium"}, []string{".config/chromium", ".var/app/org.chromium.Chromium/config/chromium", "snap/chromium/common/chromium"}},
		{Browser{Name: "brave", Label: "Brave"}, []string{".config/BraveSoftware/Brave-Browser", ".var/app/com.brave.Browser/config/BraveSoftware/Brave-Browser"}},
		{Browser{Name: "edge", Label: "Microsoft Edge"}, []string{".config/microsoft-edge"}},
		{Browser{Name: "vivaldi", Label: "Vivaldi"}, []string{".config/vivaldi"}},
		{Browser{Name: "opera", Label: "Opera"}, []string{".config/opera"}},
	}
	var out []Browser
	for _, c := range cands {
		for _, p := range c.paths {
			full := filepath.Join(h, p)
			if st, err := os.Stat(full); err == nil && st.IsDir() {
				b := c.b
				b.Path = full
				out = append(out, b)
				break
			}
		}
	}
	return out
}

// ErrNotSignedIn means the browser has no signed-in YouTube session.
var ErrNotSignedIn = errors.New("bu brauzerda YouTube'ga kirilmagan")

// BrowserClient reads the user's library through their browser session,
// so no Google Cloud project is needed.
type BrowserClient struct {
	Browser string
	ops     *innertube // like / add-to-playlist
}

// NewBrowserClient returns a client for the given yt-dlp browser name.
func NewBrowserClient(browser string) *BrowserClient {
	arg := CookieArg(browser)
	return &BrowserClient{Browser: arg, ops: &innertube{browser: arg}}
}

type playlistJSON struct {
	ID       string      `json:"id"`
	Title    string      `json:"title"`
	Channel  string      `json:"channel"`
	Uploader string      `json:"uploader"`
	Count    int64       `json:"playlist_count"`
	Entries  []flatEntry `json:"entries"`
}

func (c *BrowserClient) list(ctx context.Context, url string, limit int) (playlistJSON, error) {
	args := []string{"--flat-playlist", "-J", "--no-warnings", "--cookies-from-browser", c.Browser,
		"--extractor-args", ExtractorArgs(DefaultClients)}
	if limit > 0 {
		args = append(args, "--playlist-end", strconv.Itoa(limit))
	}
	out, err := exe(ctx, append(args, "--", url)...)
	if err != nil {
		return playlistJSON{}, browserErr(err)
	}
	var p playlistJSON
	return p, json.Unmarshal(out, &p)
}

// browserErr turns yt-dlp's cookie/sign-in failures into clear errors.
func browserErr(err error) error {
	s := err.Error()
	switch {
	case strings.Contains(s, "could not find") && strings.Contains(s, "cookies database"),
		strings.Contains(s, "failed to load cookies"):
		return errors.New("brauzer cookie'larini o'qib bo'lmadi — brauzer profili topilmadi")
	case strings.Contains(s, "secretstorage") || strings.Contains(s, "keyring") || strings.Contains(s, "decrypt"):
		return errors.New("brauzer cookie'lari shifrlangan va kalitni olib bo'lmadi (keyring)")
	case strings.Contains(s, "The playlist does not exist"), strings.Contains(s, "Sign in"),
		strings.Contains(s, "login required"), strings.Contains(s, "This playlist is private"):
		return ErrNotSignedIn
	}
	return err
}

func tracks(es []flatEntry) []track.Track {
	ts := make([]track.Track, 0, len(es))
	for _, e := range es {
		if e.ID == "" || e.Title == "[Private video]" || e.Title == "[Deleted video]" {
			continue
		}
		ts = append(ts, e.track())
	}
	return ts
}

// Me returns the signed-in channel's name (it owns the "Liked videos" list).
func (c *BrowserClient) Me(ctx context.Context) (string, error) {
	p, err := c.list(ctx, "https://www.youtube.com/playlist?list=LL", 1)
	if err != nil {
		return "", err
	}
	name := p.Channel
	if name == "" {
		name = p.Uploader
	}
	if name == "" {
		return "", ErrNotSignedIn
	}
	return name, nil
}

// Liked returns the user's liked videos, newest first.
func (c *BrowserClient) Liked(ctx context.Context) ([]track.Track, error) {
	p, err := c.list(ctx, "https://www.youtube.com/playlist?list=LL", maxLibrary)
	if err != nil {
		return nil, err
	}
	ts := tracks(p.Entries)
	for i := range ts {
		ts[i].Liked = true
	}
	return ts, nil
}

// Playlists returns the user's playlists (including Watch later and saved
// mixes); "Liked videos" is left out because it has its own entry.
func (c *BrowserClient) Playlists(ctx context.Context) ([]youtube.Playlist, error) {
	p, err := c.list(ctx, "https://www.youtube.com/feed/playlists", 0)
	if err != nil {
		return nil, err
	}
	var out []youtube.Playlist
	for _, e := range p.Entries {
		if e.ID == "" || e.ID == "LL" {
			continue
		}
		title := e.Title
		if e.ID == "WL" {
			title = "Keyinroq ko'rish"
		}
		out = append(out, youtube.Playlist{ID: e.ID, Title: title, Count: e.Count})
	}
	return out, nil
}

// PlaylistTracks returns a playlist's videos.
func (c *BrowserClient) PlaylistTracks(ctx context.Context, id string) ([]track.Track, error) {
	p, err := c.list(ctx, "https://www.youtube.com/playlist?list="+id, maxLibrary)
	if err != nil {
		return nil, err
	}
	return tracks(p.Entries), nil
}

// Rate likes or un-likes a video on the user's account.
func (c *BrowserClient) Rate(ctx context.Context, id string, like bool) error {
	return c.ops.rate(ctx, id, like)
}

// AddToPlaylist appends a video to one of the user's playlists.
func (c *BrowserClient) AddToPlaylist(ctx context.Context, playlistID, videoID string) error {
	return c.ops.addToPlaylist(ctx, playlistID, videoID)
}
