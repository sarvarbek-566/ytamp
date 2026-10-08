package ui

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"strconv"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sarvarbek-566/ytamp/internal/config"
	"github.com/sarvarbek-566/ytamp/internal/player"
	"github.com/sarvarbek-566/ytamp/internal/youtube"
)

var fuzzKeys = []string{
	"j", "k", "g", "G", "h", "l", "tab", "shift+tab", "enter", "esc", "space", "n", "p",
	"left", "right", "H", "L", "+", "-", "m", "s", "r", "z", "/", "f", "a", "e", "x", "J", "K",
	"F", "b", "R", "w", "t", "v", "o", "c", "1", "2", "3", "?", "up", "down", "pgup", "pgdown",
	"home", "end", "backspace", "delete", "ctrl+d", "ctrl+u", "y", "*", "d", "A", "A", "ctrl+o",
}

// TestFuzzRandomSessions drives the UI with long random mixes of user input,
// mpv events, network results/failures and resizes. Every step re-checks
// the invariants and the exact frame size (see harness.check).
func TestFuzzRandomSessions(t *testing.T) {
	seeds, steps := 40, 1500
	if v, err := strconv.Atoi(os.Getenv("YTAMP_FUZZ_SEEDS")); err == nil {
		seeds = v
	}
	for seed := 0; seed < seeds; seed++ {
		t.Run(fmt.Sprint("seed", seed), func(t *testing.T) {
			r := rand.New(rand.NewPCG(uint64(seed), 7))
			h := newHarness(t, hopt{cfg: func(c *config.Config) {
				c.Autoplay = r.IntN(2) == 0
				c.Visualizer = []string{"bars", "wave", "off"}[r.IntN(3)]
			}})
			if r.IntN(2) == 0 {
				h.send(clientMsg{yt: &fakeYT{liked: fakeTracks("liked", r.IntN(30)),
					pls: []youtube.Playlist{{ID: "P1", Title: "One", Count: 2}, {ID: "P2", Title: "Two", Count: 0}}}, user: "u"})
			}
			defer func() {
				if p := recover(); p != nil {
					t.Fatalf("panic at step %d: %v", h.stepN, p)
				}
			}()
			for i := 0; i < steps && !h.quit; i++ {
				fuzzStep(h, r)
			}
			h.m.Shutdown()
		})
	}
}

func fuzzStep(h *harness, r *rand.Rand) {
	switch n := r.IntN(100); {
	case n < 55:
		k := fuzzKeys[r.IntN(len(fuzzKeys))]
		if k == "q" || k == "ctrl+c" {
			return
		}
		h.keys(k)
	case n < 62: // typing while maybe in an input
		h.typeText([]string{"abc", "x", "nothing", "https://youtu.be/34Na4j8AVgA", "q", " "}[r.IntN(6)])
	case n < 64:
		h.search([]string{"abc", "def", "nothing", "song"}[r.IntN(4)])
	case n < 66:
		h.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("pasted https://youtu.be/XXYlFuWEuKI"), Paste: true})
	case n < 68:
		h.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("jjjkn")}) // coalesced runes
	case n < 80: // mpv events, including out-of-order and bogus ones
		evs := []any{
			player.LoadedEvent{},
			player.EndEvent{Reason: "eof"},
			player.EndEvent{Reason: "error"},
			player.EndEvent{Reason: "stop"},
			player.PropEvent{Name: "pause", Value: r.IntN(2) == 0},
			player.PropEvent{Name: "idle-active", Value: r.IntN(2) == 0},
			player.PropEvent{Name: "time-pos", Value: r.Float64() * 400},
			player.PropEvent{Name: "time-pos", Value: nil},
			player.PropEvent{Name: "duration", Value: r.Float64() * 400},
			player.PropEvent{Name: "volume", Value: r.Float64() * 130},
			player.PropEvent{Name: "paused-for-cache", Value: r.IntN(2) == 0},
			player.PropEvent{Name: "audio-codec-name", Value: "opus"},
			player.PropEvent{Name: "audio-bitrate", Value: 128000.0},
			player.PropEvent{Name: "unknown", Value: "?"},
		}
		h.ev(evs[r.IntN(len(evs))])
	case n < 84:
		sizes := [][2]int{{20, 5}, {50, 16}, {64, 20}, {80, 24}, {100, 30}, {120, 40}, {180, 50}, {45, 30}, {200, 18}}
		sz := sizes[r.IntN(len(sizes))]
		h.send(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
	case n < 87: // flip network failures on/off
		var e error
		if r.IntN(2) == 0 {
			e = errors.New("network is unreachable")
		}
		switch r.IntN(3) {
		case 0:
			h.searchErr = e
		case 1:
			h.radioErr = e
		default:
			h.streamErr = e
		}
		h.radioN = r.IntN(30)
	case n < 90:
		ops := []string{"playpause", "play", "pause", "next", "prev", "stop", "seek", "setpos"}
		h.send(mprisMsg{op: ops[r.IntN(len(ops))], arg: r.Int64N(300_000_000) - 50_000_000})
	case n < 93:
		h.send(tickMsg(h.m.now.Add(1200e6)))
	case n < 95:
		h.send(tea.MouseMsg{Button: []tea.MouseButton{tea.MouseButtonWheelUp, tea.MouseButtonWheelDown}[r.IntN(2)]})
	case n < 97: // late/stale async results
		h.send(searchMsg{seq: r.IntN(3), q: "late", tracks: fakeTracks("late", r.IntN(5))})
		h.send(radioMsg{seed: "zzzzzzzzzzz", tracks: fakeTracks("lr", r.IntN(5)), queue: r.IntN(2) == 0})
		h.send(lyricsMsg{id: "nope"})
	default:
		h.send(retryMsg{id: h.cur().ID, url: "https://s/x"})
	}
}
