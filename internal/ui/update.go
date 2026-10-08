package ui

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sarvarbek-566/ytamp/internal/auth"
	"github.com/sarvarbek-566/ytamp/internal/lyrics"
	"github.com/sarvarbek-566/ytamp/internal/player"
	"github.com/sarvarbek-566/ytamp/internal/store"
	"github.com/sarvarbek-566/ytamp/internal/theme"
	"github.com/sarvarbek-566/ytamp/internal/track"
	"github.com/sarvarbek-566/ytamp/internal/ytdl"
)

var sleepSteps = []time.Duration{0, 15 * time.Minute, 30 * time.Minute, 60 * time.Minute, 90 * time.Minute}

// Update handles every message.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		return m, nil

	case tickMsg:
		return m, m.onTick(time.Time(msg))

	case playerMsg:
		return m, tea.Batch(m.onPlayer(msg.ev), m.waitPlayer())

	case playerDeadMsg:
		return m, nil

	case mprisMsg:
		return m, tea.Batch(m.onMpris(msg), m.waitMpris())

	case tea.KeyMsg:
		// Fast typing or key-repeat can coalesce runes ("jjj") into one
		// message; outside text inputs, handle each rune as its own key.
		inInput := m.searchOn || m.filterOn
		if msg.Paste && !inInput && m.scr == screenMain && !m.help && !m.picker {
			// pasting a link or text outside an input starts a search with it
			m.openSearch()
			m.input.SetValue(strings.TrimSpace(string(msg.Runes)))
			m.input.CursorEnd()
			return m, nil
		}
		if msg.Type == tea.KeyRunes && len(msg.Runes) > 1 && !msg.Paste && !inInput {
			var cmds []tea.Cmd
			for _, r := range msg.Runes {
				cmds = append(cmds, m.onKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}))
			}
			return m, tea.Batch(cmds...)
		}
		return m, m.onKey(msg)

	case tea.MouseMsg:
		return m, m.onMouse(msg)

	case deviceMsg:
		return m, m.onDevice(msg)

	case tokenMsg:
		if m.scr != screenLogin || m.stage != stageDevice {
			return m, nil
		}
		if msg.err != nil {
			m.loginErr = errText(msg.err)
			return m, nil
		}
		m.cancel = nil
		m.leaveLogin()
		m.flash("✓ Google akkaunt ulandi", 1)
		m.connecting = true
		return m, m.connect(msg.tok)

	case browserMsg:
		return m, m.onBrowser(msg)

	case clientMsg:
		if msg.err != nil {
			m.connecting = false
			m.flash(errText(msg.err), 2)
			if strings.Contains(msg.err.Error(), "invalid_grant") {
				_ = auth.Logout()
			}
			return m, nil
		}
		return m, m.signedIn(msg.yt, msg.user)

	case libMsg:
		m.loading, m.libBusy = "", false
		if msg.err != nil {
			m.flash(errText(msg.err), 2)
			return m, nil
		}
		msg.lib.User = m.user
		m.lib = msg.lib
		_ = store.SaveLibrary(m.lib)
		cur := m.sources[m.srcCur].kind
		m.rebuildSources()
		if cur == srcLiked || cur == srcHistory && len(m.list) == 0 {
			m.selectSource(0, false)
		}
		m.flash("Kutubxona yangilandi · "+itoa(len(m.lib.Liked))+" ta qo'shiq", 1)
		return m, nil

	case plTracksMsg:
		m.loading = ""
		if msg.err != nil {
			m.flash(errText(msg.err), 2)
			return m, nil
		}
		m.lib.Tracks[msg.id] = msg.tracks
		_ = store.SaveLibrary(m.lib)
		if s := m.sources[m.srcCur]; s.kind == srcPlaylist && s.id == msg.id {
			m.setList(msg.tracks)
		}
		return m, nil

	case searchMsg:
		if msg.seq != m.searchSeq { // a newer search superseded this one
			return m, nil
		}
		m.loading = ""
		if msg.err != nil {
			m.flash(errText(msg.err), 2)
			return m, nil
		}
		m.search = msg.tracks
		m.rebuildSources()
		for i, s := range m.sources {
			if s.kind == srcSearch {
				m.selectSource(i, false)
			}
		}
		m.listTitle = "Qidiruv: " + msg.q
		m.focus, m.view = focusList, viewTracks
		if len(msg.tracks) == 0 {
			m.flash("Hech narsa topilmadi", 0)
		} else if msg.play {
			return m, m.playFrom(msg.tracks[:1], 0)
		}
		return m, nil

	case retryMsg:
		t, ok := m.current()
		if !ok || t.ID != msg.id {
			return m, nil
		}
		if msg.err != nil || msg.url == "" {
			logf("retry failed: %v", msg.err)
			m.starting = false
			m.errStreak++
			if msg.err != nil && errors.Is(msg.err, ytdl.ErrBotCheck) {
				m.flash(errText(msg.err), 2)
			} else {
				m.flash("Ijro etib bo'lmadi: "+t.Title, 2)
			}
			if m.errStreak < 4 {
				return m, m.next(false)
			}
			m.stopAtEnd()
			return m, nil
		}
		if m.paused { // keep a restored session paused at its position
			m.pl.LoadPaused(msg.url, m.pos)
		} else {
			m.pl.Load(msg.url, m.pos)
		}
		return m, nil

	case resolveMsg:
		if msg.err != nil {
			m.flash(errText(msg.err), 2)
			return m, nil
		}
		return m, m.playFrom([]track.Track{msg.t}, 0)

	case radioMsg:
		if msg.queue {
			m.radioBusy = false
			if msg.err != nil || len(msg.tracks) == 0 {
				if m.awaitRadio {
					m.stopAtEnd()
				}
				return m, nil
			}
			seen := map[string]bool{}
			for _, t := range m.queue {
				seen[t.ID] = true
			}
			n := len(m.queue)
			if m.autoFrom < 0 || m.autoFrom > n {
				m.autoFrom = n
			}
			for _, t := range msg.tracks {
				if !seen[t.ID] {
					m.queue = append(m.queue, t)
				}
			}
			m.radio = msg.tracks
			m.rebuildSources()
			if m.awaitRadio && len(m.queue) > n { // queue had ended; continue into the radio
				m.awaitRadio = false
				return m, m.playIndex(n)
			}
			if m.awaitRadio { // nothing new to play: stop gracefully
				m.stopAtEnd()
			}
			return m, nil
		}
		if msg.seed != m.radioSeed { // superseded by a newer radio request
			return m, nil
		}
		m.loading = ""
		if msg.err != nil {
			m.flash(errText(msg.err), 2)
			return m, nil
		}
		m.radio = msg.tracks
		m.rebuildSources()
		if m.sources[m.srcCur].kind == srcRadio {
			m.setList(msg.tracks)
		}
		return m, nil

	case lyricsMsg:
		if t, ok := m.current(); ok && t.ID == msg.id {
			m.lyrFor = msg.id
			if msg.err != nil {
				m.lyr, m.lyrErr = nil, "Bu qo'shiq uchun matn topilmadi"
				if msg.err != lyrics.ErrNotFound {
					m.lyrErr = "Matnni yuklab bo'lmadi"
				}
			} else {
				m.lyr, m.lyrErr = msg.l, ""
			}
		}
		return m, nil

	case rateMsg:
		if msg.err != nil {
			m.flash(errText(msg.err), 2)
			return m, nil
		}
		if msg.like {
			if !m.isLiked(msg.t.ID) {
				m.lib.Liked = append([]track.Track{msg.t}, m.lib.Liked...)
			}
			m.flash("♥ Yoqtirilganlarga qo'shildi", 1)
		} else {
			out := m.lib.Liked[:0]
			for _, t := range m.lib.Liked {
				if t.ID != msg.t.ID {
					out = append(out, t)
				}
			}
			m.lib.Liked = out
			m.flash("Yoqtirilganlardan olib tashlandi", 0)
		}
		_ = store.SaveLibrary(m.lib)
		m.rebuildSources()
		if m.sources[m.srcCur].kind == srcLiked {
			m.setList(m.lib.Liked)
		}
		return m, nil

	case addPlMsg:
		if msg.err != nil {
			m.flash(errText(msg.err), 2)
		} else {
			m.flash("✓ "+msg.title, 1)
		}
		return m, nil
	}
	return m, nil
}

func (m *Model) onTick(now time.Time) tea.Cmd {
	dt := 0.0
	if !m.now.IsZero() {
		dt = now.Sub(m.now).Seconds()
	}
	m.frame++
	m.now = now
	if !m.paused && !m.idle && !m.starting && !m.buffering && dt < 1 {
		m.pos += dt // interpolate between mpv's position updates
		if m.dur > 0 {
			m.pos = min(m.pos, m.dur)
		}
	}
	if now.Sub(m.lastSlow) < time.Second {
		return m.tick()
	}
	m.lastSlow = now // once-a-second housekeeping below
	m.mp.SetPosition(int64(m.pos * 1e6))
	if !m.sleepAt.IsZero() {
		left := m.sleepAt.Sub(now)
		switch {
		case left <= 0:
			m.pl.SetPause(true)
			m.sleepAt = time.Time{}
			m.setVolume(m.sleepVol)
			m.flash("☾ Uyqu taymeri: pauza. Xayrli tun!", 0)
		case left < 30*time.Second: // gentle fade-out
			m.pl.SetVolume(m.sleepVol * left.Seconds() / 30)
		}
	}
	if m.vizMode != 2 && audioCapture && !m.paused && !m.idle && !m.starting {
		m.an.Ensure() // reattach if mpv recreated its audio stream
	}
	if !m.paused && time.Since(m.lastSave) > 20*time.Second {
		m.saveSession()
	}
	return m.tick()
}

func (m *Model) onPlayer(ev any) tea.Cmd {
	switch e := ev.(type) {
	case player.PropEvent:
		f, _ := e.Value.(float64)
		b, _ := e.Value.(bool)
		switch e.Name {
		case "time-pos":
			if e.Value != nil {
				m.pos = f
			}
		case "duration":
			if f > 0 {
				m.dur = f
			}
		case "pause":
			m.paused = b
			if !m.idle {
				m.mp.SetStatus(map[bool]string{true: "Paused", false: "Playing"}[b])
			}
		case "volume":
			if m.sleepAt.IsZero() {
				m.volume = f
			}
			m.an.SetVolume(f)
		case "mute":
			m.muted = b
		case "idle-active":
			// mpv reports idle=true at startup and between files; while we
			// are loading a file that's noise, not a real stop.
			if !(b && m.starting) {
				m.idle = b
			}
		case "paused-for-cache":
			m.buffering = b
		case "audio-codec-name":
			m.codec, _ = e.Value.(string)
		case "audio-bitrate":
			if f > 0 {
				m.bitrate = f
			}
		}
	case player.LoadedEvent:
		m.starting = false
		if m.seekTo >= 0 { // apply a seek the user made while it was loading
			m.pl.SeekTo(m.seekTo)
			m.pos, m.seekTo = m.seekTo, -1
		}
		if m.vizMode != 2 && audioCapture {
			m.an.Ensure()
		}
		m.errStreak = 0
	case player.EndEvent:
		switch e.Reason {
		case "eof":
			return m.next(true)
		case "error":
			t, _ := m.current()
			logf("mpv end-file error: %s (%s)", e.Err, t.URL())
			if m.retried != t.ID { // first failure: retry once with a fresh direct URL
				m.retried = t.ID
				id, f := t.ID, player.Format(m.cfg.AudioQuality)
				ytdlStreamURL := ytdlStreamURL
				return func() tea.Msg {
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					u, err := ytdlStreamURL(ctx, id, f)
					return retryMsg{id, u, err}
				}
			}
			m.starting = false
			m.errStreak++
			m.flash("Ijro etib bo'lmadi: "+t.Title, 2)
			if m.errStreak < 4 {
				return m.next(false)
			}
			m.stopAtEnd()
			m.flash("Ketma-ket bir necha qo'shiq ijro etilmadi — internetni tekshiring", 2)
		}
	case player.ExitEvent:
		m.pl, m.plDead = noPlayer{}, true
		m.flash("mpv to'xtadi — ytamp'ni qayta ishga tushiring", 2)
	}
	return nil
}

func (m *Model) onMpris(e mprisMsg) tea.Cmd {
	switch e.op {
	case "playpause":
		return m.playPause()
	case "play":
		if m.paused {
			return m.playPause()
		}
	case "pause", "stop":
		m.pl.SetPause(true)
	case "next":
		return m.next(false)
	case "prev":
		return m.prev()
	case "seek":
		m.seek(float64(e.arg) / 1e6)
	case "setpos":
		m.seekAbs(float64(e.arg) / 1e6)
	case "quit":
		m.quitting = true
		return tea.Quit
	}
	return nil
}

func (m *Model) onMouse(e tea.MouseMsg) tea.Cmd {
	if m.scr != screenMain || m.help || m.picker {
		return nil
	}
	switch e.Button {
	case tea.MouseButtonWheelUp:
		return m.move(-3)
	case tea.MouseButtonWheelDown:
		return m.move(3)
	}
	return nil
}

// ---------- keys ----------

func (m *Model) onKey(k tea.KeyMsg) tea.Cmd {
	key := k.String()
	if key == "ctrl+c" {
		m.quitting = true
		return tea.Quit
	}

	if m.scr == screenLogin {
		return m.onLoginKey(k)
	}

	if m.help {
		switch key {
		case "?", "esc", "q":
			m.help = false
		case "j", "down":
			m.helpOff++
		case "k", "up":
			m.helpOff = max(0, m.helpOff-1)
		}
		return nil
	}

	if m.picker {
		return m.onPickerKey(key)
	}

	if m.searchOn {
		switch key {
		case "esc":
			m.searchOn = false
			m.input.Blur()
		case "enter":
			q := strings.TrimSpace(m.input.Value())
			m.searchOn = false
			m.input.Blur()
			if q == "" {
				return nil
			}
			if videoID(q) != "" {
				return m.playQuery(q)
			}
			return m.doSearch(q, false)
		default:
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(k)
			return cmd
		}
		return nil
	}

	if m.filterOn {
		switch key {
		case "esc":
			m.filterOn = false
			m.filter.Blur()
			m.filter.SetValue("")
			m.applyFilter()
		case "enter":
			m.filterOn = false
			m.filter.Blur()
			if _, i, ok := m.selected(); ok {
				return m.playFrom(m.list, i)
			}
		case "up", "down", "ctrl+n", "ctrl+p":
			return m.move(map[string]int{"up": -1, "ctrl+p": -1, "down": 1, "ctrl+n": 1}[key])
		default:
			var cmd tea.Cmd
			m.filter, cmd = m.filter.Update(k)
			m.applyFilter()
			return cmd
		}
		return nil
	}

	switch key {
	case "q":
		m.quitting = true
		return tea.Quit
	case "?":
		m.help, m.helpOff = true, 0
	case "A":
		m.openLogin(true)
	case "esc":
		if m.filter.Value() != "" {
			m.filter.SetValue("")
			m.applyFilter()
		}

	// navigation
	case "tab":
		if m.focus == focusSide {
			m.focus = focusList
		} else {
			m.focus = focusSide
		}
	case "shift+tab":
		m.focus = focusSide
	case "j", "down":
		return m.move(1)
	case "k", "up":
		return m.move(-1)
	case "g", "home":
		return m.move(-1 << 20)
	case "G", "end":
		return m.move(1 << 20)
	case "ctrl+d", "pgdown":
		return m.move(m.pageSize())
	case "ctrl+u", "pgup":
		return m.move(-m.pageSize())
	case "h":
		m.focus = focusSide
	case "l":
		m.focus = focusList
	case "1":
		m.view = viewTracks
	case "2":
		m.view = viewQueue
		m.qCur = m.qi
	case "3":
		m.view = viewLyrics
	case "enter":
		return m.activate()

	// playback
	case " ", "space":
		return m.playPause()
	case "n":
		return m.next(false)
	case "p":
		return m.prev()
	case "right":
		m.seek(5)
	case "left":
		m.seek(-5)
	case "shift+right", "L":
		m.seek(30)
	case "shift+left", "H":
		m.seek(-30)
	case "+", "=":
		m.setVolume(m.volume + 5)
	case "-", "_":
		m.setVolume(m.volume - 5)
	case "m":
		m.pl.ToggleMute()
	case "s":
		m.toggleShuffle()
		m.flash(map[bool]string{true: "⇄ Aralash: yoqildi", false: "⇄ Aralash: o'chirildi"}[m.shuffle], 0)
	case "r":
		m.repeat = (m.repeat + 1) % 3
		m.mp.SetModes(m.shuffle, loopName(m.repeat))
		m.flash([...]string{"↻ Takrorlash: o'chiq", "↻ Takrorlash: hammasi", "↻ Takrorlash: bitta qo'shiq"}[m.repeat], 0)
	case "z":
		m.cycleSleep()

	// library actions
	case "/":
		return m.openSearch()
	case "f":
		m.view = viewTracks
		m.focus = focusList
		m.filterOn = true
		return m.filter.Focus()
	case "a":
		m.enqueue(false)
	case "e":
		m.enqueue(true)
	case "x", "d", "delete":
		if m.view == viewQueue {
			m.removeFromQueue()
		}
	case "J":
		if m.view == viewQueue {
			m.moveInQueue(1)
		}
	case "K":
		if m.view == viewQueue {
			m.moveInQueue(-1)
		}
	case "*", "F":
		return m.toggleLike()
	case "b":
		if m.yt == nil || len(m.lib.Playlists) == 0 {
			m.flash("Playlistga qo'shish uchun akkauntga kiring", 2)
			return nil
		}
		if _, ok := m.selectedOrCurrent(); !ok {
			m.flash("Avval qo'shiqni tanlang", 0)
			return nil
		}
		m.picker, m.pickerCur = true, 0
	case "R":
		if m.yt == nil {
			m.flash("Akkaunt ulanmagan (ytamp --login)", 2)
			return nil
		}
		if m.libBusy {
			return nil
		}
		m.loading = "Kutubxona yangilanmoqda"
		return m.fetchLibrary()
	case "w":
		if t, ok := m.selectedOrCurrent(); ok {
			m.radio = nil
			for i, s := range m.sources {
				if s.kind == srcRadio {
					m.srcCur = i
				}
			}
			m.view, m.focus = viewTracks, focusList
			m.listCur, m.listOff = 0, 0
			m.listTitle = "Radio · " + t.Title
			m.setList(nil)
			return m.loadRadio(t.ID, false)
		}

	// misc
	case "t":
		m.theme = theme.Next(m.theme)
		m.st = newStyles(theme.Get(m.theme))
		m.flash("Mavzu: "+m.theme, 0)
	case "v":
		m.vizMode = (m.vizMode + 1) % 3
	case "o":
		if t, ok := m.selectedOrCurrent(); ok {
			openURL(t.URL())
		}
	case "c", "y":
		if t, ok := m.selectedOrCurrent(); ok {
			copyText(t.URL())
			m.flash("Havola nusxalandi", 1)
		}
	}
	return nil
}

func (m *Model) onPickerKey(key string) tea.Cmd {
	n := len(m.lib.Playlists)
	if n == 0 {
		m.picker = false
		return nil
	}
	m.pickerCur = max(0, min(m.pickerCur, n-1))
	switch key {
	case "esc", "q", "b":
		m.picker = false
	case "j", "down":
		m.pickerCur = (m.pickerCur + 1) % n
	case "k", "up":
		m.pickerCur = (m.pickerCur - 1 + n) % n
	case "enter":
		m.picker = false
		return m.addToPlaylist(m.lib.Playlists[m.pickerCur])
	}
	return nil
}

func (m *Model) pageSize() int { return max(1, m.h/2) }

func (m *Model) move(d int) tea.Cmd {
	if m.focus == focusSide {
		n := m.srcCur + d
		n = max(0, min(len(m.sources)-1, n))
		if n != m.srcCur {
			return m.selectSource(n, false)
		}
		return nil
	}
	switch m.view {
	case viewQueue:
		m.qCur = max(0, min(len(m.queue)-1, m.qCur+d))
	case viewTracks:
		m.listCur = max(0, min(len(m.shown)-1, m.listCur+d))
	}
	return nil
}

func (m *Model) activate() tea.Cmd {
	if m.focus == focusSide {
		cmd := m.selectSource(m.srcCur, true)
		m.focus = focusList
		return cmd
	}
	switch m.view {
	case viewQueue:
		return m.playIndex(m.qCur)
	case viewTracks:
		if _, i, ok := m.selected(); ok {
			return m.playFrom(m.list, i)
		}
	}
	return nil
}

// openSearch shows the search box on the track list, from any view.
func (m *Model) openSearch() tea.Cmd {
	m.filterOn = false
	m.filter.Blur()
	m.view, m.focus = viewTracks, focusList
	m.searchOn = true
	m.input.SetValue("")
	return m.input.Focus()
}

// playPause toggles playback, (re)starting the queue when nothing is loaded.
func (m *Model) playPause() tea.Cmd {
	switch {
	case len(m.queue) == 0:
		return nil
	case m.ended:
		if m.qi+1 < len(m.queue) { // something was queued after the end
			return m.playIndex(m.qi + 1)
		}
		return m.playIndex(m.qi)
	case m.idle && !m.starting:
		return m.playIndex(m.qi)
	}
	m.pl.TogglePause()
	return nil
}

func (m *Model) seek(sec float64) {
	if m.starting && m.seekTo < 0 {
		m.seekTo = m.pos
	}
	m.seekAbs(m.posOrPending() + sec)
}

func (m *Model) posOrPending() float64 {
	if m.seekTo >= 0 {
		return m.seekTo
	}
	return m.pos
}

// seekAbs jumps to sec; while a file is still loading the target is kept
// and applied once mpv reports it loaded.
func (m *Model) seekAbs(sec float64) {
	if (m.idle && !m.starting) || m.ended || len(m.queue) == 0 {
		return
	}
	sec = max(0, sec)
	if m.dur > 0 {
		sec = min(sec, m.dur-1)
	}
	if m.starting {
		m.seekTo, m.pos = sec, sec
		return
	}
	m.pl.SeekTo(sec)
	m.pos = sec
	m.mp.Seeked(int64(sec * 1e6))
}

func (m *Model) enqueue(next bool) {
	t, _, ok := m.selected()
	if m.view != viewTracks || !ok {
		return
	}
	if len(m.queue) == 0 {
		m.queue, m.qi, m.qCur = []track.Track{t}, 0, 0
		m.flash("Navbatga qo'shildi: "+t.Title, 1)
		m.move(1)
		return
	}
	if next {
		at := m.qi + 1
		m.queue = append(m.queue[:at], append([]track.Track{t}, m.queue[at:]...)...)
		m.flash("Keyingi bo'lib qo'yildi: "+t.Title, 1)
	} else if m.autoFrom > m.qi && m.autoFrom < len(m.queue) {
		// keep the user's picks ahead of tracks the radio added
		at := m.autoFrom
		m.queue = append(m.queue[:at], append([]track.Track{t}, m.queue[at:]...)...)
		m.flash("Navbatga qo'shildi: "+t.Title, 1)
	} else {
		m.queue = append(m.queue, t)
		m.flash("Navbatga qo'shildi: "+t.Title, 1)
	}
	if m.autoFrom > m.qi && m.autoFrom < len(m.queue)-0 {
		m.autoFrom++
	}
	m.move(1)
}

func (m *Model) removeFromQueue() {
	i := m.qCur
	if i < 0 || i >= len(m.queue) || i == m.qi {
		if i == m.qi {
			m.flash("Hozir ijrodagi qo'shiqni olib bo'lmaydi", 0)
		}
		return
	}
	m.queue = append(m.queue[:i], m.queue[i+1:]...)
	if i < m.qi {
		m.qi--
	}
	if i < m.autoFrom {
		m.autoFrom--
	}
	m.qCur = max(0, min(m.qCur, len(m.queue)-1))
}

func (m *Model) moveInQueue(d int) {
	i, j := m.qCur, m.qCur+d
	if i < 0 || j < 0 || j >= len(m.queue) {
		return
	}
	m.queue[i], m.queue[j] = m.queue[j], m.queue[i]
	switch m.qi {
	case i:
		m.qi = j
	case j:
		m.qi = i
	}
	m.qCur = j
}

func (m *Model) cycleSleep() {
	cur := 0
	if !m.sleepAt.IsZero() {
		left := time.Until(m.sleepAt)
		for i, s := range sleepSteps {
			if s > 0 && left <= s {
				cur = i
				break
			}
		}
	}
	nxt := (cur + 1) % len(sleepSteps)
	if m.sleepAt.IsZero() {
		m.sleepVol = m.volume
	} else {
		m.setVolume(m.sleepVol)
	}
	if sleepSteps[nxt] == 0 {
		m.sleepAt = time.Time{}
		m.flash("☾ Uyqu taymeri o'chirildi", 0)
		return
	}
	m.sleepAt = time.Now().Add(sleepSteps[nxt])
	m.flash("☾ Uyqu taymeri: "+itoa(int(sleepSteps[nxt].Minutes()))+" daqiqa", 0)
}

// ---------- os helpers ----------

func download(url, path string) error {
	c := &http.Client{Timeout: 6 * time.Second}
	res, err := c.Get(url)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return io.ErrUnexpectedEOF
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, res.Body)
	return err
}

func itoa(n int) string { return strconv.Itoa(n) }
