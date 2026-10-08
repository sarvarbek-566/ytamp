// Package ui is ytamp's Bubble Tea terminal interface.
package ui

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/sarvarbek-566/ytamp/internal/auth"
	"github.com/sarvarbek-566/ytamp/internal/config"
	"github.com/sarvarbek-566/ytamp/internal/lyrics"
	"github.com/sarvarbek-566/ytamp/internal/mpris"
	"github.com/sarvarbek-566/ytamp/internal/player"
	"github.com/sarvarbek-566/ytamp/internal/store"
	"github.com/sarvarbek-566/ytamp/internal/theme"
	"github.com/sarvarbek-566/ytamp/internal/track"
	"github.com/sarvarbek-566/ytamp/internal/viz"
	"github.com/sarvarbek-566/ytamp/internal/youtube"
	"github.com/sarvarbek-566/ytamp/internal/ytdl"
	"golang.org/x/oauth2"
)

type screen int

const (
	screenLogin screen = iota
	screenMain
)

type focus int

const (
	focusSide focus = iota
	focusList
)

type view int

const (
	viewTracks view = iota
	viewQueue
	viewLyrics
)

type srcKind int

const (
	srcLiked srcKind = iota
	srcHistory
	srcSearch
	srcRadio
	srcPlaylist
)

type source struct {
	kind  srcKind
	title string
	id    string
	count int
}

const (
	repeatOff = iota
	repeatAll
	repeatOne
)

type toast struct {
	text    string
	kind    int // 0 info, 1 ok, 2 error
	expires time.Time
}

// Options passed from main.
type Options struct {
	Config     config.Config
	ForceLogin bool
	Guest      bool
	PlayQuery  string // "ytamp <query|url>" plays immediately
}

// Model is the root Bubble Tea model.
type Model struct {
	cfg   config.Config
	opts  Options
	st    styles
	theme string

	w, h  int
	scr   screen
	frame int
	now   time.Time

	// auth
	oc         *oauth2.Config
	da         *oauth2.DeviceAuthResponse
	loginErr   string
	yt         ytAPI
	user       string
	cancel     context.CancelFunc
	connecting bool // token present, waiting for the API to answer

	// login screen (login.go)
	stage         loginStage
	loginCur      int
	loginReturn   bool
	browsers      []ytdl.Browser
	tryingBrowser ytdl.Browser
	idIn, secIn   textinput.Model
	formFocus     int
	pendingCreds  bool // OAuth client typed in the form, not yet accepted by Google
	libBusy       bool

	// library
	lib       store.Library
	history   []store.HistoryEntry
	sources   []source
	likedSet  map[string]bool
	srcCur    int
	focus     focus
	view      view
	list      []track.Track
	listTitle string
	listCur   int
	listOff   int
	loading   string // non-empty → spinner with this label
	search    []track.Track
	radio     []track.Track
	radioSeed string
	searchSeq int // ignore results of superseded searches

	filterOn bool
	filter   textinput.Model
	shown    []int // indices into list after filtering

	searchOn bool
	input    textinput.Model

	// playback
	pl         mediaPlayer
	plDead     bool
	ended      bool    // queue played to the end and stopped
	autoFrom   int     // queue index where autoplay-radio tracks start (-1: none)
	seekTo     float64 // seek requested while the file was still loading (-1: none)
	queue      []track.Track
	orig       []track.Track // pre-shuffle order
	qi         int
	qCur       int
	shuffle    bool
	repeat     int
	pos, dur   float64
	paused     bool
	idle       bool
	buffering  bool
	starting   bool
	volume     float64
	muted      bool
	codec      string
	bitrate    float64
	errStreak  int
	retried    string // track ID already retried via a fresh stream URL
	radioBusy  bool
	awaitRadio bool // queue ended; play the radio as soon as it arrives

	// extras
	lyr      *lyrics.Lyrics
	lyrFor   string
	lyrErr   string
	an       *viz.Analyzer
	vizMode  int // 0 bars, 1 mirror, 2 off
	toast    toast
	help     bool
	helpOff  int
	sleepAt  time.Time
	sleepVol float64
	mp       *mpris.Server
	mpCh     chan mprisMsg

	picker    bool // add-to-playlist overlay
	pickerCur int

	lastSave time.Time
	lastSlow time.Time
	quitting bool
}

// New builds the model; player and MPRIS are started in Init.
func New(o Options) *Model {
	in := textinput.New()
	in.Prompt = "  "
	in.Placeholder = "qo'shiq, ijrochi yoki YouTube havola…"
	in.CharLimit = 200
	fl := textinput.New()
	fl.Prompt = "  "
	fl.Placeholder = "filtr…"

	m := &Model{
		cfg: o.Config, opts: o, theme: o.Config.Theme,
		input: in, filter: fl, focus: focusList,
		volume: float64(o.Config.Volume), mpCh: make(chan mprisMsg, 16),
		paused: true, idle: true, pl: noPlayer{}, an: viz.New(), autoFrom: -1, seekTo: -1,
	}
	switch o.Config.Visualizer {
	case "wave":
		m.vizMode = 1
	case "off":
		m.vizMode = 2
	}
	if s, err := store.LoadSession(); err == nil && s.Theme != "" {
		m.theme = s.Theme
	}
	m.st = newStyles(theme.Get(m.theme))
	m.history, _ = store.LoadHistory()
	m.lib, _ = store.LoadLibrary()
	return m
}

// ---------- messages ----------

type (
	tickMsg       time.Time
	playerMsg     struct{ ev any }
	playerDeadMsg struct{}
	mprisMsg      struct {
		op  string
		arg int64
	}
	deviceMsg struct {
		da  *oauth2.DeviceAuthResponse
		err error
	}
	tokenMsg struct {
		tok *oauth2.Token
		err error
	}
	clientMsg struct {
		yt   ytAPI
		user string
		err  error
	}
	libMsg struct {
		lib store.Library
		err error
	}
	plTracksMsg struct {
		id     string
		tracks []track.Track
		err    error
	}
	searchMsg struct {
		seq    int
		q      string
		tracks []track.Track
		err    error
		play   bool
	}
	radioMsg struct {
		seed   string
		tracks []track.Track
		err    error
		queue  bool // append to queue (autoplay) vs. show as list
	}
	lyricsMsg struct {
		id  string
		l   *lyrics.Lyrics
		err error
	}
	rateMsg struct {
		t    track.Track
		like bool
		err  error
	}
	addPlMsg struct {
		title string
		err   error
	}
	retryMsg struct {
		id  string
		url string
		err error
	}
	resolveMsg struct {
		t   track.Track
		err error
	}
)

// tick schedules the next frame: 30 fps while music (and the visualizer) is
// moving, 8 fps otherwise — enough for spinners, light on CPU.
func (m *Model) tick() tea.Cmd {
	d := time.Second / 8
	if m.scr == screenMain && !m.paused && !m.idle && m.vizMode != 2 {
		d = time.Second / 30
	}
	return tickFn(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *Model) waitPlayer() tea.Cmd {
	ch := m.pl.Events()
	if ch == nil { // no engine (or a test fake): nothing to wait for
		return nil
	}
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return playerDeadMsg{}
		}
		return playerMsg{ev}
	}
}

func (m *Model) waitMpris() tea.Cmd {
	if !pumpMpris {
		return nil
	}
	return func() tea.Msg { return <-m.mpCh }
}

// ---------- init ----------

// Init starts mpv, the analyzer, MPRIS and auth.
func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.tick(), m.waitMpris(), tea.SetWindowTitle("ytamp")}

	pl, err := startPlayer(player.Options{SocketDir: config.RuntimeDir(), Volume: m.cfg.Volume, Quality: m.cfg.AudioQuality, Device: m.cfg.AudioDevice,
		Clients: ytdl.DefaultClients, Cookies: ytdl.CookieArg(m.cfg.CookiesFromBrowser)})
	if err != nil {
		m.pl, m.plDead = noPlayer{}, true
		m.flash(err.Error(), 2)
	} else {
		m.pl = pl
		cmds = append(cmds, m.waitPlayer())
	}
	if mp, err := startMpris(handler{m.mpCh}); err == nil {
		m.mp = mp
	}

	if m.cfg.ResumeSession {
		m.restoreSession()
	}

	if oc, err := auth.OAuthConfig(m.cfg); err == nil {
		m.oc = oc
	}
	tok, terr := auth.LoadToken()
	switch {
	case m.opts.Guest:
		m.enterMain()
	case m.opts.ForceLogin:
		m.enterMain() // so leaving the login screen lands on a ready player
		m.openLogin(false)
	case m.oc != nil && terr == nil: // OAuth account
		m.connecting = true
		m.enterMain()
		cmds = append(cmds, m.connect(tok))
	case m.cfg.CookiesFromBrowser != "": // browser-session account
		m.connecting = true
		m.enterMain()
		cmds = append(cmds, m.connectBrowser(m.cfg.CookiesFromBrowser))
	default:
		m.enterMain()
		m.flash("Mehmon rejimi · YouTube akkauntga kirish: A", 0)
	}
	if m.opts.PlayQuery != "" {
		cmds = append(cmds, m.playQuery(m.opts.PlayQuery))
	}
	return tea.Batch(cmds...)
}

func (m *Model) enterMain() {
	m.scr = screenMain
	m.rebuildSources()
	if len(m.list) == 0 {
		m.selectSource(0, false)
	}
}

func (m *Model) startDevice() tea.Cmd {
	if m.cancel != nil { // abandon a previous code's polling
		m.cancel()
	}
	oc := m.oc
	if oc == nil {
		return func() tea.Msg { return deviceMsg{err: auth.ErrNoClient} }
	}
	deviceStart := deviceStart // bind now: the cmd runs on another goroutine
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		da, err := deviceStart(ctx, oc)
		return deviceMsg{da, err}
	}
}

func (m *Model) waitToken() tea.Cmd {
	oc, da := m.oc, m.da
	if oc == nil || da == nil {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	deviceWait := deviceWait
	return func() tea.Msg {
		tok, err := deviceWait(ctx, oc, da)
		return tokenMsg{tok, err}
	}
}

func (m *Model) connect(tok *oauth2.Token) tea.Cmd {
	oc := m.oc
	return func() tea.Msg {
		ctx := context.Background()
		c, err := youtube.New(ctx, auth.TokenSource(ctx, oc, tok))
		if err != nil {
			return clientMsg{err: err}
		}
		tctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		user, err := c.Me(tctx)
		if err != nil {
			return clientMsg{err: err}
		}
		return clientMsg{yt: c, user: user}
	}
}

func (m *Model) fetchLibrary() tea.Cmd {
	if m.libBusy || m.yt == nil {
		return nil
	}
	m.libBusy = true
	c, old := m.yt, m.lib
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		liked, err := c.Liked(ctx)
		if err != nil {
			return libMsg{err: err}
		}
		pls, err := c.Playlists(ctx)
		if err != nil {
			return libMsg{err: err}
		}
		lib := store.Library{User: old.User, Liked: liked, Playlists: pls, Tracks: map[string][]track.Track{}, Updated: time.Now()}
		// keep cached playlist tracks whose item count didn't change
		for _, p := range pls {
			if ts, ok := old.Tracks[p.ID]; ok {
				for _, op := range old.Playlists {
					if op.ID == p.ID && op.Count == p.Count {
						lib.Tracks[p.ID] = ts
					}
				}
			}
		}
		return libMsg{lib: lib}
	}
}

// ---------- sources & lists ----------

func (m *Model) rebuildSources() {
	m.likedSet = make(map[string]bool, len(m.lib.Liked))
	for _, t := range m.lib.Liked {
		m.likedSet[t.ID] = true
	}
	var s []source
	if m.yt != nil || len(m.lib.Liked) > 0 {
		s = append(s, source{kind: srcLiked, title: "♥ Yoqtirilganlar", count: len(m.lib.Liked)})
	}
	s = append(s,
		source{kind: srcHistory, title: "◷ Tarix", count: len(m.history)},
		source{kind: srcSearch, title: "⌕ Qidiruv", count: len(m.search)},
		source{kind: srcRadio, title: "✦ Radio", count: len(m.radio)},
	)
	for _, p := range m.lib.Playlists {
		s = append(s, source{kind: srcPlaylist, title: "♫ " + p.Title, id: p.ID, count: int(p.Count)})
	}
	var prev source
	if m.srcCur < len(m.sources) {
		prev = m.sources[m.srcCur]
	}
	m.sources = s
	m.srcCur = min(m.srcCur, len(s)-1)
	for i, src := range s {
		if prev.title != "" && src.kind == prev.kind && src.id == prev.id {
			m.srcCur = i
			break
		}
	}
}

// selectSource shows source i; activate=true (Enter) may also open the search
// box or fetch a radio, which plain j/k browsing must not do.
func (m *Model) selectSource(i int, activate bool) tea.Cmd {
	if i < 0 || i >= len(m.sources) {
		return nil
	}
	m.srcCur = i
	src := m.sources[i]
	m.view = viewTracks
	m.listCur, m.listOff = 0, 0
	m.listTitle = strings.TrimSpace(src.title[strings.Index(src.title, " ")+1:])
	m.loading = ""
	switch src.kind {
	case srcLiked:
		m.setList(m.lib.Liked)
	case srcHistory:
		ts := make([]track.Track, len(m.history))
		for i, h := range m.history {
			ts[i] = h.Track
		}
		m.setList(ts)
	case srcSearch:
		m.setList(m.search)
		if len(m.search) == 0 && activate {
			m.searchOn = true
			m.input.SetValue("")
			return m.input.Focus()
		}
	case srcRadio:
		m.setList(m.radio)
		if len(m.radio) == 0 && activate {
			if t, ok := m.current(); ok {
				return m.loadRadio(t.ID, false)
			}
			m.flash("Radio uchun avval biror qo'shiq qo'ying", 0)
		}
	case srcPlaylist:
		if ts, ok := m.lib.Tracks[src.id]; ok {
			m.setList(ts)
		} else if m.yt != nil {
			m.setList(nil)
			m.loading = "Playlist yuklanmoqda"
			c, id := m.yt, src.id
			return func() tea.Msg {
				ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				defer cancel()
				ts, err := c.PlaylistTracks(ctx, id)
				return plTracksMsg{id, ts, err}
			}
		}
	}
	return nil
}

func (m *Model) setList(ts []track.Track) {
	m.list = ts
	m.applyFilter()
}

func (m *Model) applyFilter() {
	q := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	m.shown = m.shown[:0]
	for i, t := range m.list {
		if q == "" || strings.Contains(strings.ToLower(t.Title+" "+t.Artist), q) {
			m.shown = append(m.shown, i)
		}
	}
	m.listCur = max(0, min(m.listCur, len(m.shown)-1))
}

func (m *Model) selected() (track.Track, int, bool) {
	if m.listCur < 0 || m.listCur >= len(m.shown) {
		return track.Track{}, -1, false
	}
	i := m.shown[m.listCur]
	return m.list[i], i, true
}

func (m *Model) current() (track.Track, bool) {
	if m.qi < 0 || m.qi >= len(m.queue) {
		return track.Track{}, false
	}
	return m.queue[m.qi], true
}

func (m *Model) isLiked(id string) bool { return m.likedSet[id] }

// ---------- playback ----------

func (m *Model) playIndex(i int) tea.Cmd {
	if i < 0 || i >= len(m.queue) {
		return nil
	}
	m.qi, m.qCur = i, i
	m.awaitRadio, m.ended = false, false
	t := m.queue[i]
	m.pl.Load(t.URL(), 0)
	m.pos, m.dur = 0, t.Duration.Seconds()
	m.starting, m.paused, m.idle = true, false, false
	m.codec, m.bitrate = "", 0
	m.retried, m.seekTo = "", -1
	m.lyr, m.lyrErr, m.lyrFor = nil, "", ""
	m.history = store.AddHistory(m.history, t)
	m.refreshHistory()
	m.mp.SetTrack(t)
	m.mp.SetStatus("Playing")
	m.saveSession()
	cmds := []tea.Cmd{m.fetchLyrics(t), tea.SetWindowTitle("▶ " + t.Title + " — " + t.Artist)}
	if m.cfg.Notifications {
		cmds = append(cmds, notify(t))
	}
	if m.cfg.Autoplay && i >= len(m.queue)-2 && m.repeat == repeatOff {
		cmds = append(cmds, m.loadRadio(t.ID, true))
	}
	return tea.Batch(cmds...)
}

// playFrom replaces the queue with list and starts at index i.
func (m *Model) playFrom(list []track.Track, i int) tea.Cmd {
	m.queue = append([]track.Track(nil), list...)
	m.orig, m.autoFrom = nil, -1
	if m.shuffle {
		m.orig = append([]track.Track(nil), m.queue...)
		first := m.queue[i]
		rest := append(append([]track.Track(nil), m.queue[:i]...), m.queue[i+1:]...)
		rand.Shuffle(len(rest), func(a, b int) { rest[a], rest[b] = rest[b], rest[a] })
		m.queue = append([]track.Track{first}, rest...)
		i = 0
	}
	m.errStreak = 0
	return m.playIndex(i)
}

func (m *Model) next(auto bool) tea.Cmd {
	if len(m.queue) == 0 {
		return nil
	}
	if auto && m.repeat == repeatOne {
		return m.playIndex(m.qi)
	}
	if m.qi+1 < len(m.queue) {
		return m.playIndex(m.qi + 1)
	}
	if m.repeat == repeatAll {
		return m.playIndex(0)
	}
	if m.cfg.Autoplay {
		if t, ok := m.current(); ok {
			m.starting, m.awaitRadio = true, true
			return m.loadRadio(t.ID, true)
		}
	}
	m.stopAtEnd()
	return nil
}

// stopAtEnd stops playback after the last track; a later space/n continues
// with anything queued after it.
func (m *Model) stopAtEnd() {
	m.pl.Stop()
	m.paused, m.starting, m.awaitRadio, m.ended = true, false, false, true
	m.pos = 0
	m.mp.SetStatus("Stopped")
}

// refreshHistory keeps the History source in sync while it is on screen.
func (m *Model) refreshHistory() {
	cur := m.srcCur < len(m.sources) && m.sources[m.srcCur].kind == srcHistory
	m.rebuildSources()
	if !cur || m.view != viewTracks {
		return
	}
	sel, _, _ := m.selected()
	ts := make([]track.Track, len(m.history))
	for i, h := range m.history {
		ts[i] = h.Track
	}
	m.setList(ts)
	for vi, li := range m.shown { // keep the cursor on the same song
		if m.list[li].ID == sel.ID {
			m.listCur = vi
			break
		}
	}
}

func (m *Model) prev() tea.Cmd {
	if len(m.queue) == 0 {
		return nil
	}
	if m.pos > 4 || m.qi == 0 { // restart the current song
		m.seekAbs(0)
		return nil
	}
	return m.playIndex(m.qi - 1)
}

func (m *Model) toggleShuffle() {
	m.shuffle = !m.shuffle
	if len(m.queue) > 1 {
		if m.shuffle {
			m.orig = append([]track.Track(nil), m.queue...)
			rest := m.queue[m.qi+1:]
			rand.Shuffle(len(rest), func(a, b int) { rest[a], rest[b] = rest[b], rest[a] })
		} else if m.orig != nil {
			m.unshuffle()
		}
	}
	m.mp.SetModes(m.shuffle, loopName(m.repeat))
}

// unshuffle restores the original order while keeping whatever was added to
// (or removed from) the queue during shuffle; the current track stays current.
func (m *Model) unshuffle() {
	cur, hasCur := m.current()
	inQueue := map[string]int{}
	for _, t := range m.queue {
		inQueue[t.ID]++
	}
	var out []track.Track
	for _, t := range m.orig {
		if inQueue[t.ID] > 0 {
			inQueue[t.ID]--
			out = append(out, t)
		}
	}
	for _, t := range m.queue { // tracks added while shuffled go last
		if inQueue[t.ID] > 0 {
			inQueue[t.ID]--
			out = append(out, t)
		}
	}
	m.queue, m.orig, m.autoFrom = out, nil, -1
	if hasCur {
		for i, t := range m.queue {
			if t.ID == cur.ID {
				m.qi = i
				break
			}
		}
	}
	m.qCur = m.qi
}

func loopName(r int) string {
	return [...]string{"None", "Playlist", "Track"}[r]
}

func (m *Model) setVolume(v float64) {
	m.volume = max(0, min(130, v))
	m.pl.SetVolume(m.volume)
	m.mp.SetVolume(m.volume / 100)
}

func (m *Model) loadRadio(seed string, toQueue bool) tea.Cmd {
	if toQueue {
		if m.radioBusy {
			return nil
		}
		m.radioBusy = true
	} else {
		m.loading = "Radio tayyorlanmoqda"
		m.radioSeed = seed
	}
	ytdlRadio := ytdlRadio
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		ts, err := ytdlRadio(ctx, seed, 25)
		return radioMsg{seed: seed, tracks: ts, err: err, queue: toQueue}
	}
}

func (m *Model) doSearch(q string, play bool) tea.Cmd {
	m.loading = "Qidirilmoqda: " + q
	m.searchSeq++
	n, seq := m.cfg.SearchResults, m.searchSeq
	ytdlSearch := ytdlSearch
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		ts, err := ytdlSearch(ctx, q, n)
		return searchMsg{seq: seq, q: q, tracks: ts, err: err, play: play}
	}
}

// playQuery plays a URL directly, or searches and plays the top hit.
func (m *Model) playQuery(q string) tea.Cmd {
	if id := videoID(q); id != "" {
		ytdlResolve := ytdlResolve
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			t, err := ytdlResolve(ctx, id)
			return resolveMsg{t, err}
		}
	}
	return m.doSearch(q, true)
}

func (m *Model) fetchLyrics(t track.Track) tea.Cmd {
	lyricsFetch := lyricsFetch
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		l, err := lyricsFetch(ctx, t.Artist, t.Title, t.Duration)
		return lyricsMsg{t.ID, l, err}
	}
}

func (m *Model) toggleLike() tea.Cmd {
	t, ok := m.current()
	if !ok {
		return nil
	}
	if m.yt == nil {
		m.flash("Yoqtirish uchun YouTube akkauntga kiring", 2)
		return nil
	}
	like := !m.isLiked(t.ID)
	c := m.yt
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return rateMsg{t, like, c.Rate(ctx, t.ID, like)}
	}
}

func (m *Model) addToPlaylist(p youtube.Playlist) tea.Cmd {
	t, ok := m.selectedOrCurrent()
	if !ok {
		return nil
	}
	c := m.yt
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		err := c.AddToPlaylist(ctx, p.ID, t.ID)
		return addPlMsg{title: t.Title + " → " + p.Title, err: err}
	}
}

// selectedOrCurrent is the track an action like o/c/w/b applies to: the
// highlighted row of the visible list, else the playing track.
func (m *Model) selectedOrCurrent() (track.Track, bool) {
	if m.focus == focusList {
		switch m.view {
		case viewTracks:
			if t, _, ok := m.selected(); ok {
				return t, true
			}
		case viewQueue:
			if m.qCur >= 0 && m.qCur < len(m.queue) {
				return m.queue[m.qCur], true
			}
		}
	}
	return m.current()
}

// ---------- session ----------

func (m *Model) restoreSession() {
	s, err := store.LoadSession()
	if err != nil || len(s.Queue) == 0 || m.plDead {
		return
	}
	m.queue = s.Queue
	m.autoFrom = -1
	m.qi = max(0, min(s.Index, len(s.Queue)-1))
	m.qCur = m.qi
	m.shuffle, m.repeat = s.Shuffle, max(0, min(2, s.Repeat))
	if s.Volume > 0 {
		m.volume = s.Volume
		m.pl.SetVolume(s.Volume)
	}
	t := m.queue[m.qi]
	m.pl.LoadPaused(t.URL(), s.Position)
	m.pos, m.dur = max(0, s.Position), t.Duration.Seconds()
	m.paused, m.idle, m.starting = true, false, true
	m.mp.SetTrack(t)
	m.mp.SetStatus("Paused")
	m.mp.SetModes(m.shuffle, loopName(m.repeat))
}

func (m *Model) saveSession() {
	m.lastSave = time.Now()
	q := m.queue
	if len(q) > 500 { // keep the session file small
		lo := max(0, m.qi-100)
		q = q[lo:min(len(q), lo+500)]
		_ = store.SaveSession(store.Session{Queue: q, Index: m.qi - lo, Position: m.pos, Volume: m.volume, Shuffle: m.shuffle, Repeat: m.repeat, Theme: m.theme})
		return
	}
	_ = store.SaveSession(store.Session{Queue: q, Index: m.qi, Position: m.pos, Volume: m.volume, Shuffle: m.shuffle, Repeat: m.repeat, Theme: m.theme})
}

// Shutdown persists state and stops child processes.
func (m *Model) Shutdown() {
	if m.cancel != nil {
		m.cancel()
	}
	if m.scr == screenMain && len(m.queue) > 0 { // never clobber a good session with an empty one
		m.saveSession()
	}
	m.pl.Close()
	m.an.Stop()
	m.mp.Close()
}

// ---------- helpers ----------

var logFile *os.File

// logf appends a line to ~/.local/state/ytamp/ytamp.log for troubleshooting.
func logf(format string, a ...any) {
	if logFile == nil {
		_ = os.MkdirAll(config.StateDir(), 0o700)
		f, err := os.OpenFile(filepath.Join(config.StateDir(), "ytamp.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return
		}
		logFile = f
	}
	fmt.Fprintf(logFile, time.Now().Format("2006-01-02 15:04:05 ")+format+"\n", a...)
}

func (m *Model) flash(text string, kind int) {
	if kind == 2 {
		logf("error: %s", text)
	}
	m.toast = toast{text: text, kind: kind, expires: time.Now().Add(4 * time.Second)}
}

func notify(t track.Track) tea.Cmd {
	return func() tea.Msg {
		if _, err := exec.LookPath("notify-send"); err != nil {
			return nil
		}
		icon := thumbPath(t)
		_ = exec.Command("notify-send", "-a", "ytamp", "-i", icon,
			"-h", "string:x-canonical-private-synchronous:ytamp", "-t", "4000",
			t.Title, t.Artist).Run()
		return nil
	}
}

// thumbPath downloads (once) and returns a local thumbnail, or a stock icon.
func thumbPath(t track.Track) string {
	dir := filepath.Join(config.StateDir(), "thumbs")
	p := filepath.Join(dir, t.ID+".jpg")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	_ = os.MkdirAll(dir, 0o700)
	if err := download(t.Thumb(), p); err != nil {
		return "audio-x-generic"
	}
	return p
}

func videoID(s string) string {
	s = strings.TrimSpace(s)
	for _, pre := range []string{"https://", "http://", "www.", "m.", "music."} {
		s = strings.TrimPrefix(s, pre)
	}
	switch {
	case strings.HasPrefix(s, "youtu.be/"):
		s = strings.TrimPrefix(s, "youtu.be/")
	case strings.HasPrefix(s, "youtube.com/watch"):
		_, v, ok := strings.Cut(s, "v=")
		if !ok {
			return ""
		}
		s = v
	case strings.HasPrefix(s, "youtube.com/shorts/"):
		s = strings.TrimPrefix(s, "youtube.com/shorts/")
	default:
		return ""
	}
	if i := strings.IndexAny(s, "&?#/"); i >= 0 {
		s = s[:i]
	}
	if len(s) != 11 {
		return ""
	}
	return s
}

func errText(err error) string {
	if errors.Is(err, ytdl.ErrBotCheck) {
		return "YouTube bot tekshiruvi so'radi — config.toml'da cookies_from_browser = \"firefox\" qo'ying"
	}
	s := err.Error()
	switch {
	case strings.Contains(s, "quotaExceeded"):
		return "YouTube API kunlik kvotasi tugadi (ertaga tiklanadi)"
	case strings.Contains(s, "invalid_client"):
		return "Google bu Client ID/secret'ni tanimadi — esc bilan qaytib tekshiring"
	case strings.Contains(s, "access_denied"):
		return "Ruxsat berilmadi — qayta urinib ko'ring"
	case strings.Contains(s, "expired_token"):
		return "Kod muddati tugadi — r bosib yangisini oling"
	case strings.Contains(s, "invalid_grant"):
		return "Sessiya eskirgan — qayta kiring: ytamp --login"
	case strings.Contains(s, "HTTP Error 429"):
		return "YouTube so'rovlarni vaqtincha chekladi — birozdan keyin urinib ko'ring"
	case strings.Contains(s, "no such host"), strings.Contains(s, "network is unreachable"),
		strings.Contains(s, "Network is unreachable"), strings.Contains(s, "Unable to download"),
		strings.Contains(s, "Failed to resolve"), strings.Contains(s, "name resolution"),
		strings.Contains(s, "HTTPSConnection"), strings.Contains(s, "connection refused"):
		return "Internet aloqasi yo'q"
	}
	if len(s) > 90 {
		s = s[:90] + "…"
	}
	return s
}

// handler adapts MPRIS callbacks into Bubble Tea messages.
type handler struct{ ch chan mprisMsg }

func (h handler) send(op string, arg int64) {
	select {
	case h.ch <- mprisMsg{op, arg}:
	default:
	}
}
func (h handler) MprisPlayPause()           { h.send("playpause", 0) }
func (h handler) MprisPlay()                { h.send("play", 0) }
func (h handler) MprisPause()               { h.send("pause", 0) }
func (h handler) MprisNext()                { h.send("next", 0) }
func (h handler) MprisPrev()                { h.send("prev", 0) }
func (h handler) MprisStop()                { h.send("stop", 0) }
func (h handler) MprisSeek(us int64)        { h.send("seek", us) }
func (h handler) MprisSetPosition(us int64) { h.send("setpos", us) }
func (h handler) MprisQuit()                { h.send("quit", 0) }
