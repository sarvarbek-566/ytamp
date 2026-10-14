// Package store persists library cache, listening history and the session.
package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/voidsecurity404/ytamp/internal/config"
	"github.com/voidsecurity404/ytamp/internal/track"
	"github.com/voidsecurity404/ytamp/internal/youtube"
)

const historyMax = 300

// Library is the cached YouTube library (saves API quota on startup).
type Library struct {
	User      string                   `json:"user"`
	Liked     []track.Track            `json:"liked"`
	Playlists []youtube.Playlist       `json:"playlists"`
	Tracks    map[string][]track.Track `json:"tracks"` // playlist id → tracks
	Updated   time.Time                `json:"updated"`
}

// Session is the playback state restored on next launch.
type Session struct {
	Queue    []track.Track `json:"queue"`
	Index    int           `json:"index"`
	Position float64       `json:"position"`
	Volume   float64       `json:"volume"`
	Shuffle  bool          `json:"shuffle"`
	Repeat   int           `json:"repeat"`
	Theme    string        `json:"theme"`
}

// HistoryEntry is one play.
type HistoryEntry struct {
	track.Track
	At time.Time `json:"at"`
}

func path(name string) string { return filepath.Join(config.StateDir(), name) }

func load(name string, v any) error {
	b, err := os.ReadFile(path(name))
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func save(name string, v any) error {
	if err := os.MkdirAll(config.StateDir(), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tmp := path(name) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path(name))
}

func LoadLibrary() (Library, error) {
	var l Library
	err := load("library.json", &l)
	if l.Tracks == nil {
		l.Tracks = map[string][]track.Track{}
	}
	return l, err
}
func SaveLibrary(l Library) error   { return save("library.json", l) }
func ClearLibrary()                 { _ = os.Remove(path("library.json")) }
func LoadSession() (Session, error) { var s Session; return s, load("session.json", &s) }
func SaveSession(s Session) error   { return save("session.json", s) }
func LoadHistory() ([]HistoryEntry, error) {
	var h []HistoryEntry
	return h, load("history.json", &h)
}

// AddHistory moves t to the top of the history (one entry per track).
func AddHistory(h []HistoryEntry, t track.Track) []HistoryEntry {
	out := make([]HistoryEntry, 0, min(len(h)+1, historyMax))
	out = append(out, HistoryEntry{Track: t, At: time.Now()})
	for _, e := range h {
		if e.ID != t.ID && len(out) < historyMax {
			out = append(out, e)
		}
	}
	_ = save("history.json", out)
	return out
}
