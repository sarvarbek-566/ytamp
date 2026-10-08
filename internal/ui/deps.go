package ui

import (
	"context"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sarvarbek-566/ytamp/internal/lyrics"
	"github.com/sarvarbek-566/ytamp/internal/mpris"
	"github.com/sarvarbek-566/ytamp/internal/player"
	"github.com/sarvarbek-566/ytamp/internal/track"
	"github.com/sarvarbek-566/ytamp/internal/youtube"
	"github.com/sarvarbek-566/ytamp/internal/ytdl"
)

// mediaPlayer is what the UI needs from the audio engine (mpv in production,
// a recorder in tests).
type mediaPlayer interface {
	Events() <-chan any
	Load(url string, start float64)
	LoadPaused(url string, start float64)
	TogglePause()
	SetPause(bool)
	Seek(sec float64)
	SeekTo(sec float64)
	SetVolume(v float64)
	ToggleMute()
	Stop()
	Close()
}

// ytAPI is the subset of the YouTube Data API client the UI uses.
type ytAPI interface {
	Me(ctx context.Context) (string, error)
	Liked(ctx context.Context) ([]track.Track, error)
	Playlists(ctx context.Context) ([]youtube.Playlist, error)
	PlaylistTracks(ctx context.Context, id string) ([]track.Track, error)
	Rate(ctx context.Context, id string, like bool) error
	AddToPlaylist(ctx context.Context, playlistID, videoID string) error
}

// noPlayer stands in when mpv couldn't start or has died, so no code path
// ever calls into a nil player.
type noPlayer struct{}

func (noPlayer) Events() <-chan any         { return nil }
func (noPlayer) Load(string, float64)       {}
func (noPlayer) LoadPaused(string, float64) {}
func (noPlayer) TogglePause()               {}
func (noPlayer) SetPause(bool)              {}
func (noPlayer) Seek(float64)               {}
func (noPlayer) SeekTo(float64)             {}
func (noPlayer) SetVolume(float64)          {}
func (noPlayer) ToggleMute()                {}
func (noPlayer) Stop()                      {}
func (noPlayer) Close()                     {}

// Swappable side effects; tests replace these with fakes.
var (
	startPlayer = func(o player.Options) (mediaPlayer, error) {
		p, err := player.Start(o)
		if err != nil {
			return nil, err
		}
		return p, nil
	}
	startMpris    = mpris.New
	ytdlSearch    = ytdl.Search
	ytdlRadio     = ytdl.Radio
	ytdlResolve   = ytdl.Resolve
	ytdlStreamURL = ytdl.StreamURL
	lyricsFetch   = lyrics.Fetch
	audioCapture  = true // start pw-record for the visualizer
	pumpMpris     = true
	tickFn        = tea.Tick

	openURL = func(url string) {
		go func() { _ = exec.Command("xdg-open", url).Run() }()
	}
	copyText = func(s string) {
		go func() {
			for _, c := range [][]string{{"wl-copy"}, {"xclip", "-selection", "clipboard"}} {
				if _, err := exec.LookPath(c[0]); err == nil {
					cmd := exec.Command(c[0], c[1:]...)
					cmd.Stdin = strings.NewReader(s)
					_ = cmd.Run()
					return
				}
			}
		}()
	}
)
