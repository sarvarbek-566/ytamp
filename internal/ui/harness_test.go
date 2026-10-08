package ui

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/sarvarbek-566/ytamp/internal/config"
	"github.com/sarvarbek-566/ytamp/internal/lyrics"
	"github.com/sarvarbek-566/ytamp/internal/mpris"
	"github.com/sarvarbek-566/ytamp/internal/player"
	"github.com/sarvarbek-566/ytamp/internal/track"
	"github.com/sarvarbek-566/ytamp/internal/youtube"
	"github.com/sarvarbek-566/ytamp/internal/ytdl"
	"golang.org/x/oauth2"
)

// ---------- fakes ----------

type fakePlayer struct {
	mu    sync.Mutex
	calls []string
	ev    chan any
}

func (f *fakePlayer) rec(s string)       { f.mu.Lock(); f.calls = append(f.calls, s); f.mu.Unlock() }
func (f *fakePlayer) Events() <-chan any { return f.ev }
func (f *fakePlayer) Load(u string, s float64) {
	f.rec(fmt.Sprintf("load %s @%.0f", u, s))
}
func (f *fakePlayer) LoadPaused(u string, s float64) {
	f.rec(fmt.Sprintf("loadpaused %s @%.0f", u, s))
}
func (f *fakePlayer) TogglePause()        { f.rec("toggle") }
func (f *fakePlayer) SetPause(b bool)     { f.rec(fmt.Sprintf("pause %v", b)) }
func (f *fakePlayer) Seek(s float64)      { f.rec(fmt.Sprintf("seek %.0f", s)) }
func (f *fakePlayer) SeekTo(s float64)    { f.rec(fmt.Sprintf("seekto %.0f", s)) }
func (f *fakePlayer) SetVolume(v float64) { f.rec(fmt.Sprintf("vol %.0f", v)) }
func (f *fakePlayer) ToggleMute()         { f.rec("mute") }
func (f *fakePlayer) Stop()               { f.rec("stop") }
func (f *fakePlayer) Close()              { f.rec("close") }

func (f *fakePlayer) last(prefix string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.calls) - 1; i >= 0; i-- {
		if strings.HasPrefix(f.calls[i], prefix) {
			return f.calls[i]
		}
	}
	return ""
}

func (f *fakePlayer) count(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func (f *fakePlayer) reset() { f.mu.Lock(); f.calls = nil; f.mu.Unlock() }

// id makes a unique 11-char YouTube-like id for s.
func id(s string) string {
	f := fnv.New64a()
	f.Write([]byte(s))
	return fmt.Sprintf("%016x", f.Sum64())[:11]
}

func fakeTracks(prefix string, n int) []track.Track {
	ts := make([]track.Track, n)
	for i := range ts {
		ts[i] = track.Track{ID: id(fmt.Sprintf("%s%d", prefix, i)), Title: fmt.Sprintf("%s song %d", prefix, i),
			Artist: "Artist " + prefix, Duration: time.Duration(150+i) * time.Second}
	}
	return ts
}

type fakeYT struct {
	mu        sync.Mutex
	liked     []track.Track
	pls       []youtube.Playlist
	plTracks  map[string][]track.Track
	rated     map[string]bool
	added     []string
	likedCall int
	fail      error
}

func (y *fakeYT) Me(context.Context) (string, error) { return "sar1k", y.fail }
func (y *fakeYT) Liked(context.Context) ([]track.Track, error) {
	y.mu.Lock()
	y.likedCall++
	y.mu.Unlock()
	return y.liked, y.fail
}
func (y *fakeYT) Playlists(context.Context) ([]youtube.Playlist, error) { return y.pls, y.fail }
func (y *fakeYT) PlaylistTracks(_ context.Context, id string) ([]track.Track, error) {
	return y.plTracks[id], y.fail
}
func (y *fakeYT) Rate(_ context.Context, id string, like bool) error {
	y.mu.Lock()
	defer y.mu.Unlock()
	if y.rated == nil {
		y.rated = map[string]bool{}
	}
	y.rated[id] = like
	return y.fail
}
func (y *fakeYT) AddToPlaylist(_ context.Context, pl, vid string) error {
	y.mu.Lock()
	defer y.mu.Unlock()
	y.added = append(y.added, pl+":"+vid)
	return y.fail
}

// ---------- harness ----------

type harness struct {
	t      *testing.T
	m      *Model
	pl     *fakePlayer
	quit   bool
	opened []string
	copied []string
	saved  map[string]string // config.Set calls
	// browser login fakes
	browserYT  *fakeYT
	browserErr error
	// knobs for fakes
	searchErr error
	radioErr  error
	streamErr error
	radioN    int
	stepN     int
}

type hopt struct {
	cfg      func(*config.Config)
	opts     func(*Options)
	stateDir string
}

func newHarness(t *testing.T, o hopt) *harness {
	t.Helper()
	dir := o.stateDir
	if dir == "" {
		dir = t.TempDir()
	}
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())

	h := &harness{t: t, pl: &fakePlayer{}, radioN: 25}
	save := []any{startPlayer, startMpris, ytdlSearch, ytdlRadio, ytdlResolve, ytdlStreamURL, lyricsFetch, audioCapture, openURL, copyText, pumpMpris, tickFn,
		detectBrowsers, newBrowserClient, saveConfig, deviceStart, deviceWait}
	t.Cleanup(func() {
		startPlayer = save[0].(func(player.Options) (mediaPlayer, error))
		startMpris = save[1].(func(mpris.Handler) (*mpris.Server, error))
		ytdlSearch = save[2].(func(context.Context, string, int) ([]track.Track, error))
		ytdlRadio = save[3].(func(context.Context, string, int) ([]track.Track, error))
		ytdlResolve = save[4].(func(context.Context, string) (track.Track, error))
		ytdlStreamURL = save[5].(func(context.Context, string, string) (string, error))
		lyricsFetch = save[6].(func(context.Context, string, string, time.Duration) (*lyrics.Lyrics, error))
		audioCapture = save[7].(bool)
		openURL = save[8].(func(string))
		copyText = save[9].(func(string))
		pumpMpris = save[10].(bool)
		tickFn = save[11].(func(time.Duration, func(time.Time) tea.Msg) tea.Cmd)
		detectBrowsers = save[12].(func() []ytdl.Browser)
		newBrowserClient = save[13].(func(string) ytAPI)
		saveConfig = save[14].(func(string, string) error)
		deviceStart = save[15].(func(context.Context, *oauth2.Config) (*oauth2.DeviceAuthResponse, error))
		deviceWait = save[16].(func(context.Context, *oauth2.Config, *oauth2.DeviceAuthResponse) (*oauth2.Token, error))
	})
	startPlayer = func(player.Options) (mediaPlayer, error) { return h.pl, nil }
	startMpris = func(mpris.Handler) (*mpris.Server, error) { return nil, errors.New("no dbus in tests") }
	ytdlSearch = func(_ context.Context, q string, n int) ([]track.Track, error) {
		if h.searchErr != nil {
			return nil, h.searchErr
		}
		if q == "nothing" {
			return nil, nil
		}
		return fakeTracks(q, min(n, 12)), nil
	}
	ytdlRadio = func(_ context.Context, seed string, n int) ([]track.Track, error) {
		if h.radioErr != nil {
			return nil, h.radioErr
		}
		return fakeTracks("radio-"+seed, min(n, h.radioN)), nil
	}
	ytdlResolve = func(_ context.Context, v string) (track.Track, error) {
		return track.Track{ID: v, Title: "Resolved " + v, Artist: "Someone", Duration: 200 * time.Second}, nil
	}
	ytdlStreamURL = func(_ context.Context, v, _ string) (string, error) {
		if h.streamErr != nil {
			return "", h.streamErr
		}
		return "https://stream.example/" + v, nil
	}
	lyricsFetch = func(_ context.Context, a, title string, d time.Duration) (*lyrics.Lyrics, error) {
		if strings.Contains(title, "song 1") {
			return nil, lyrics.ErrNotFound
		}
		return &lyrics.Lyrics{Synced: true, Lines: []lyrics.Line{{At: 0, Text: "first"}, {At: 5 * time.Second, Text: "second"}, {At: 9 * time.Second, Text: ""}}}, nil
	}
	audioCapture, pumpMpris = false, false
	tickFn = func(time.Duration, func(time.Time) tea.Msg) tea.Cmd { return nil }
	h.saved = map[string]string{}
	h.browserYT = &fakeYT{liked: fakeTracks("bliked", 7), pls: []youtube.Playlist{{ID: "WL", Title: "Keyinroq ko'rish"}}}
	detectBrowsers = func() []ytdl.Browser {
		return []ytdl.Browser{{Name: "firefox", Label: "Firefox"}, {Name: "chromium", Label: "Chromium"}}
	}
	newBrowserClient = func(b string) ytAPI {
		if h.browserErr != nil {
			return &fakeYT{fail: h.browserErr}
		}
		return h.browserYT
	}
	saveConfig = func(k, v string) error { h.saved[k] = v; return nil }
	deviceStart = func(context.Context, *oauth2.Config) (*oauth2.DeviceAuthResponse, error) {
		return nil, errors.New("no network in tests")
	}
	deviceWait = func(ctx context.Context, _ *oauth2.Config, _ *oauth2.DeviceAuthResponse) (*oauth2.Token, error) {
		<-ctx.Done() // polls until cancelled, like the real thing
		return nil, ctx.Err()
	}
	openURL = func(u string) { h.opened = append(h.opened, u) }
	copyText = func(s string) { h.copied = append(h.copied, s) }

	cfg := config.Config{Theme: "tokyonight", Volume: 50, AudioQuality: "best", Autoplay: true,
		Visualizer: "bars", ResumeSession: true, SearchResults: 30}
	if o.cfg != nil {
		o.cfg(&cfg)
	}
	opts := Options{Config: cfg, Guest: true}
	if o.opts != nil {
		o.opts(&opts)
	}
	h.m = New(opts)
	h.run(h.m.Init())
	h.send(tea.WindowSizeMsg{Width: 120, Height: 40})
	return h
}

// run executes cmds like the Bubble Tea runtime would, feeding results back
// into Update. Commands that block (event pumps, timers) are abandoned after
// a short wait; ticks are dropped so tests stay deterministic.
func (h *harness) run(cmd tea.Cmd) {
	h.runDepth(cmd, 0)
}

func (h *harness) runDepth(cmd tea.Cmd, depth int) {
	if cmd == nil || depth > 60 {
		return
	}
	leaves := []tea.Cmd{cmd}
	results := make(chan tea.Msg, len(leaves))
	var wg sync.WaitGroup
	for _, c := range leaves {
		wg.Add(1)
		go func(c tea.Cmd) {
			defer wg.Done()
			done := make(chan tea.Msg, 1)
			go func() { done <- c() }()
			select {
			case msg := <-done:
				results <- msg
			case <-time.After(8 * time.Millisecond): // fakes answer instantly; anything slower is a timer (cursor blink)
			}
		}(c)
	}
	wg.Wait()
	close(results)
	for msg := range results {
		switch msg := msg.(type) {
		case nil, tickMsg:
		case tea.BatchMsg:
			for _, c := range msg {
				h.runDepth(c, depth+1)
			}
		case tea.QuitMsg:
			h.quit = true
		default:
			if fmt.Sprintf("%T", msg) == "tea.setWindowTitleMsg" {
				continue
			}
			_, next := h.m.Update(msg)
			h.runDepth(next, depth+1)
		}
	}
}

func (h *harness) send(msg tea.Msg) {
	h.t.Helper()
	h.stepN++
	_, cmd := h.m.Update(msg)
	h.run(cmd)
	h.check()
}

var keyTypes = map[string]tea.KeyType{
	"enter": tea.KeyEnter, "esc": tea.KeyEscape, "tab": tea.KeyTab, "shift+tab": tea.KeyShiftTab,
	"up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight,
	"backspace": tea.KeyBackspace, "space": tea.KeySpace, "home": tea.KeyHome, "end": tea.KeyEnd,
	"pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown, "ctrl+d": tea.KeyCtrlD, "ctrl+u": tea.KeyCtrlU,
	"shift+left": tea.KeyShiftLeft, "shift+right": tea.KeyShiftRight, "ctrl+c": tea.KeyCtrlC, "delete": tea.KeyDelete, "ctrl+o": tea.KeyCtrlO,
}

func keyMsg(k string) tea.KeyMsg {
	if t, ok := keyTypes[k]; ok {
		return tea.KeyMsg{Type: t}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

// keys sends each argument as one key press.
func (h *harness) keys(ks ...string) {
	h.t.Helper()
	for _, k := range ks {
		h.send(keyMsg(k))
	}
}

// typeText types s one rune at a time.
func (h *harness) typeText(s string) {
	h.t.Helper()
	for _, r := range s {
		h.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func (h *harness) ev(e any) { h.t.Helper(); h.send(playerMsg{e}) }

// loaded simulates mpv starting the requested file.
func (h *harness) loaded() {
	h.t.Helper()
	h.ev(player.PropEvent{Name: "pause", Value: false})
	h.ev(player.LoadedEvent{})
	h.ev(player.PropEvent{Name: "idle-active", Value: false})
	h.ev(player.PropEvent{Name: "time-pos", Value: 0.0})
}

// eof simulates the current file ending naturally (mpv goes idle after).
func (h *harness) eof() {
	h.t.Helper()
	h.ev(player.EndEvent{Reason: "eof"})
}

func (h *harness) screen() string { return ansi.Strip(h.m.View()) }

func (h *harness) cur() track.Track {
	t, _ := h.m.current()
	return t
}

// check asserts invariants that must hold after every single step.
func (h *harness) check() {
	h.t.Helper()
	m := h.m
	if len(m.queue) > 0 && (m.qi < 0 || m.qi >= len(m.queue)) {
		h.t.Fatalf("step %d: qi=%d out of range (queue %d)", h.stepN, m.qi, len(m.queue))
	}
	if len(m.queue) > 0 && (m.qCur < 0 || m.qCur >= len(m.queue)) {
		h.t.Fatalf("step %d: qCur=%d out of range (queue %d)", h.stepN, m.qCur, len(m.queue))
	}
	if len(m.shown) > 0 && (m.listCur < 0 || m.listCur >= len(m.shown)) {
		h.t.Fatalf("step %d: listCur=%d out of range (%d shown)", h.stepN, m.listCur, len(m.shown))
	}
	for _, i := range m.shown {
		if i < 0 || i >= len(m.list) {
			h.t.Fatalf("step %d: shown index %d out of list %d", h.stepN, i, len(m.list))
		}
	}
	if m.srcCur < 0 || (len(m.sources) > 0 && m.srcCur >= len(m.sources)) {
		h.t.Fatalf("step %d: srcCur=%d out of range", h.stepN, m.srcCur)
	}
	if m.searchOn && m.view != viewTracks {
		h.t.Fatalf("step %d: search box open but view=%d (invisible input)", h.stepN, m.view)
	}
	if m.filterOn && m.view != viewTracks {
		h.t.Fatalf("step %d: filter open but view=%d", h.stepN, m.view)
	}
	if m.searchOn && m.filterOn {
		h.t.Fatalf("step %d: search and filter both open", h.stepN)
	}
	if m.pl == nil {
		h.t.Fatalf("step %d: nil player", h.stepN)
	}
	h.checkRender()
}

// checkRender verifies the frame is exactly w×h cells — any overflow would
// wrap in a real terminal and garble the layout.
func (h *harness) checkRender() {
	h.t.Helper()
	m := h.m
	if m.w == 0 || m.h == 0 {
		return
	}
	out := m.View()
	if m.quitting {
		return
	}
	lines := strings.Split(out, "\n")
	if len(lines) != m.h {
		h.t.Fatalf("step %d: rendered %d lines, want %d (w=%d)\n%s", h.stepN, len(lines), m.h, m.w, ansi.Strip(out))
	}
	worst, wi := 0, -1
	for i, l := range lines {
		if w := lipgloss.Width(strings.TrimRight(ansi.Strip(l), " ")); w > worst {
			worst, wi = w, i
		}
	}
	for i, l := range lines {
		if w := lipgloss.Width(l); w != m.w {
			h.t.Fatalf("step %d: line %d is %d cells wide, want %d; widest content is line %d:\n%q\nview=%d help=%v picker=%v",
				h.stepN, i, w, m.w, wi, ansi.Strip(lines[wi]), m.view, m.help, m.picker)
		}
	}
	if m.searchOn && m.scr == screenMain && !m.help && m.w >= 50 && m.h >= 16 && !strings.Contains(ansi.Strip(out), "⌕") {
		h.t.Fatalf("step %d: search is active but the search box is not on screen", h.stepN)
	}
}
