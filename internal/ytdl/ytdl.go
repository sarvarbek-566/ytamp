// Package ytdl uses yt-dlp for quota-free search, "radio" (YouTube Mix) and
// stream resolution.
package ytdl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/voidsecurity404/ytamp/internal/track"
)

// DefaultClients asks yt-dlp to also try the embedded web player, which keeps
// working when YouTube rate-limits an IP with "confirm you're not a bot".
const DefaultClients = "default,web_embedded"

// fallbackClients are tried in order when resolving a stream fails.
var fallbackClients = []string{DefaultClients, "web_embedded", "android_vr,tv", "mweb,web_safari"}

// CookiesFromBrowser, when set (e.g. "firefox"), makes yt-dlp use that
// browser's YouTube session — the last resort against bot checks.
var CookiesFromBrowser string

// ErrBotCheck means YouTube demanded a signed-in session.
var ErrBotCheck = errors.New("YouTube bot tekshiruvi")

// ExtractorArgs is the --extractor-args value for a client list.
func ExtractorArgs(clients string) string { return "youtube:player_client=" + clients }

func common(clients string) []string {
	a := []string{"--no-warnings", "--extractor-args", ExtractorArgs(clients)}
	if CookiesFromBrowser != "" {
		a = append(a, "--cookies-from-browser", CookieArg(CookiesFromBrowser))
	}
	return a
}

// exe runs yt-dlp and turns failures into a readable error (first ERROR line).
func exe(ctx context.Context, args ...string) ([]byte, error) {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "yt-dlp", args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err == nil {
		return out, nil
	}
	msg := errLine(stderr.String())
	if strings.Contains(msg, "not a bot") || strings.Contains(msg, "Sign in to confirm") {
		return nil, fmt.Errorf("%w: %s", ErrBotCheck, msg)
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("yt-dlp: %w", ctx.Err())
	}
	if msg == "" {
		return nil, fmt.Errorf("yt-dlp: %w", err)
	}
	return nil, errors.New("yt-dlp: " + msg)
}

func errLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if strings.HasPrefix(l, "ERROR:") {
			return strings.TrimSpace(strings.TrimPrefix(l, "ERROR:"))
		}
	}
	return strings.TrimSpace(firstLine([]byte(s)))
}

type flatEntry struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Channel  string  `json:"channel"`
	Uploader string  `json:"uploader"`
	Duration float64 `json:"duration"`
	Count    int64   `json:"playlist_count"`
}

func (e flatEntry) track() track.Track {
	ch := e.Channel
	if ch == "" {
		ch = e.Uploader
	}
	a, t := track.Clean(e.Title, ch)
	return track.Track{ID: e.ID, Title: t, Artist: a, Duration: time.Duration(e.Duration * float64(time.Second))}
}

func flat(ctx context.Context, args ...string) ([]track.Track, error) {
	base := append([]string{"--flat-playlist", "-J", "--ignore-errors"}, common(DefaultClients)...)
	out, err := exe(ctx, append(base, args...)...)
	if err != nil {
		return nil, err
	}
	var r struct{ Entries []flatEntry }
	if err := json.Unmarshal(out, &r); err != nil {
		return nil, err
	}
	ts := make([]track.Track, 0, len(r.Entries))
	for _, e := range r.Entries {
		if e.ID == "" || e.Title == "[Private video]" || e.Title == "[Deleted video]" {
			continue
		}
		ts = append(ts, e.track())
	}
	return ts, nil
}

// Search finds videos via yt-dlp, so it costs no API quota.
func Search(ctx context.Context, q string, n int) ([]track.Track, error) {
	return flat(ctx, "ytsearch"+strconv.Itoa(n)+":"+q)
}

// Radio returns YouTube's auto-generated Mix for a seed video, excluding it.
func Radio(ctx context.Context, seedID string, n int) ([]track.Track, error) {
	url := "https://www.youtube.com/watch?v=" + seedID + "&list=RD" + seedID
	ts, err := flat(ctx, "--playlist-end", strconv.Itoa(n+1), url)
	if err != nil {
		return nil, err
	}
	out := ts[:0]
	for _, t := range ts {
		if t.ID != seedID {
			out = append(out, t)
		}
	}
	return out, nil
}

// Resolve fetches metadata for a single video ID or URL.
func Resolve(ctx context.Context, idOrURL string) (track.Track, error) {
	args := append([]string{"-J", "--skip-download", "--no-playlist", "--ignore-no-formats-error"}, common(DefaultClients)...)
	out, err := exe(ctx, append(args, "--", idOrURL)...)
	if err != nil {
		return track.Track{}, err
	}
	var e flatEntry
	if err := json.Unmarshal(out, &e); err != nil {
		return track.Track{}, err
	}
	// with --ignore-no-formats-error an unavailable video still yields a
	// placeholder ("youtube video #ID", no duration/channel)
	if e.ID == "" || (e.Duration == 0 && e.Channel == "" && e.Uploader == "") {
		return track.Track{}, errors.New("video mavjud emas yoki ochib bo'lmaydi")
	}
	return e.track(), nil
}

// StreamURL resolves a direct audio stream URL, walking through several
// YouTube player clients until one works (used to retry failed loads).
func StreamURL(ctx context.Context, id, format string) (string, error) {
	var last error
	for _, c := range fallbackClients {
		args := append([]string{"-f", format, "-g", "--no-playlist"}, common(c)...)
		out, err := exe(ctx, append(args, "--", "https://www.youtube.com/watch?v="+id)...)
		if err == nil {
			if u := strings.TrimSpace(firstLine(out)); strings.HasPrefix(u, "http") {
				return u, nil
			}
			err = errors.New("yt-dlp: oqim havolasi bo'sh")
		}
		last = err
		if ctx.Err() != nil {
			break
		}
	}
	return "", last
}

func firstLine(b []byte) string {
	if i := bytes.IndexByte(b, '\n'); i >= 0 {
		return string(b[:i])
	}
	return string(b)
}
