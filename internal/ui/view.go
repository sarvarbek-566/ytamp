package ui

import (
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/voidsecurity404/ytamp/internal/track"
)

var spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func (m *Model) spin() string { return spinner[int(time.Now().UnixMilli()/90)%len(spinner)] }

// eq is the little animated "now playing" glyph.
func (m *Model) eq() string {
	if m.paused || m.idle {
		return "▶"
	}
	f := []string{"▁▃▅", "▂▅▃", "▃▇▂", "▅▄▆", "▆▂▄", "▄▅▂", "▂▆▅", "▃▄▇"}
	return f[int(time.Now().UnixMilli()/140)%len(f)]
}

var logo = [2]string{
	"▀▄▀ ▀█▀ ▄▀█ █▀▄▀█ █▀█",
	" █   █  █▀█ █ ▀ █ █▀▀",
}

func (m *Model) gradText(s string) string {
	rs := []rune(s)
	var b strings.Builder
	for i, r := range rs {
		if r == ' ' {
			b.WriteRune(r)
			continue
		}
		b.WriteString(lipgloss.NewStyle().Foreground(m.st.gradAt(float64(i) / float64(max(1, len(rs)-1)))).Bold(true).Render(string(r)))
	}
	return b.String()
}

// View renders the whole screen.
func (m *Model) View() string {
	if m.quitting {
		return ""
	}
	if m.w < 50 || m.h < 16 {
		msg := ansi.Truncate("Oyna juda kichik — kamida 50×16 kerak", max(1, m.w), "…")
		return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, m.st.muted.Render(msg))
	}
	if m.scr == screenLogin {
		return m.viewLogin()
	}
	if m.help {
		return m.viewHelp()
	}

	ph := m.playerHeight()
	bh := m.h - 2 - ph
	sw := 0
	switch {
	case m.w >= 100:
		sw = 28
	case m.w >= 76:
		sw = 23
	}
	cw := m.w - sw
	if sw > 0 {
		cw--
	}

	center := m.viewCenter(cw, bh)
	if m.picker {
		center = m.viewPicker(cw, bh)
	}
	body := center
	if sw > 0 {
		body = lipgloss.JoinHorizontal(lipgloss.Top, m.viewSide(sw, bh), " ", center)
	}
	return lipgloss.JoinVertical(lipgloss.Left, m.viewHeader(), body, m.viewPlayer(m.w, ph), m.viewFooter())
}

func (m *Model) playerHeight() int {
	switch {
	case m.vizMode == 2:
		return 7
	case m.h >= 40:
		return 13
	case m.h >= 32:
		return 11
	case m.h >= 24:
		return 9
	}
	return 7
}

// ---------- header ----------

func (m *Model) viewHeader() string {
	left := m.st.accent.Bold(true).Render(" ▶ ") + m.gradText("ytamp")

	tabs := []string{"Qo'shiqlar", "Navbat", "Matn"}
	var tb []string
	for i, t := range tabs {
		k := m.st.muted.Render(itoa(i + 1))
		if view(i) == m.view {
			tb = append(tb, k+" "+m.st.accent.Bold(true).Underline(true).Render(t))
		} else {
			tb = append(tb, k+" "+m.st.muted.Render(t))
		}
	}
	mid := strings.Join(tb, m.st.subtle.Render("  ·  "))

	var right []string
	if !m.sleepAt.IsZero() {
		right = append(right, m.st.accent2.Render("☾ "+track.FormatDur(time.Until(m.sleepAt).Round(time.Second))))
	}
	if m.yt != nil {
		right = append(right, m.st.good.Render("●")+" "+m.st.fg.Render(m.user))
	} else if m.connecting {
		right = append(right, m.st.warn.Render(m.spin())+" "+m.st.muted.Render(nz(m.lib.User, "ulanmoqda")))
	} else if m.oc != nil && m.lib.User != "" {
		right = append(right, m.st.warn.Render("⚠")+" "+m.st.muted.Render(m.lib.User+" · oflayn"))
	} else {
		right = append(right, m.st.muted.Render("○ mehmon · ")+m.st.key.Render("A")+m.st.muted.Render(" kirish"))
	}
	r := strings.Join(right, "  ") + " "

	gap := m.w - lipgloss.Width(left) - lipgloss.Width(mid) - lipgloss.Width(r)
	if gap < 2 {
		return fit(left+"  "+mid, m.w)
	}
	l := gap / 2
	return left + strings.Repeat(" ", l) + mid + strings.Repeat(" ", gap-l) + r
}

// ---------- sidebar ----------

func (m *Model) viewSide(w, h int) string {
	inner := w - 4
	var lines []string
	selLine := 0
	for i, s := range m.sources {
		if s.kind == srcPlaylist && (i == 0 || m.sources[i-1].kind != srcPlaylist) {
			lines = append(lines, "", m.st.muted.Bold(true).Render("PLAYLISTLAR"))
		}
		cnt := ""
		if s.count > 0 {
			cnt = itoa(s.count)
		}
		name := fit(s.title, inner-len(cnt)-1)
		row := name + " " + cnt
		switch {
		case i == m.srcCur && m.focus == focusSide:
			row = m.st.sel.Render(fit(" "+s.title, inner-len(cnt)-1) + cnt + " ")
		case i == m.srcCur:
			row = m.st.accent.Bold(true).Render(name) + " " + m.st.muted.Render(cnt)
		default:
			row = m.st.fg.Render(name) + " " + m.st.muted.Render(cnt)
		}
		if i == m.srcCur {
			selLine = len(lines)
		}
		lines = append(lines, row)
	}
	if m.yt == nil && m.lib.User == "" {
		lines = append(lines, "", m.st.muted.Render("Akkauntga kirib"), m.st.muted.Render("playlistlaringizni"), m.st.muted.Render("oching: ytamp --login"))
	}
	vis := h - 2
	off := 0
	if selLine >= vis {
		off = selLine - vis + 1
	}
	if off > 0 && off < len(lines) {
		lines = lines[off:]
	}
	return m.st.box("Kutubxona", "", lines, w, h, m.focus == focusSide)
}

// ---------- center ----------

func (m *Model) viewCenter(w, h int) string {
	switch m.view {
	case viewQueue:
		return m.viewQueue(w, h)
	case viewLyrics:
		return m.viewLyrics(w, h)
	}
	return m.viewTracks(w, h)
}

func (m *Model) columns(inner int) (num, title, artist, dur int) {
	num, dur = 5, 9
	rest := inner - num - dur - 3
	artist = max(8, rest*2/5)
	if inner < 60 {
		artist = max(0, rest/3)
	}
	title = rest - artist
	return
}

func (m *Model) viewTracks(w, h int) string {
	inner := w - 4
	var lines []string
	if m.searchOn {
		lines = append(lines, m.st.accent.Render("⌕")+m.input.View(), m.st.subtle.Render(strings.Repeat("─", inner)))
	} else if m.filterOn || m.filter.Value() != "" {
		lines = append(lines, m.st.accent.Render("⧩")+m.filter.View(), m.st.subtle.Render(strings.Repeat("─", inner)))
	}

	if m.loading != "" && len(m.shown) == 0 {
		lines = append(lines, "", "", lipgloss.PlaceHorizontal(inner, lipgloss.Center, m.st.accent.Render(m.spin())+" "+m.st.fg.Render(m.loading+"…")))
		return m.st.box(m.listTitle, "", lines, w, h, m.focus == focusList)
	}
	if len(m.shown) == 0 {
		lines = append(lines, "", "")
		for _, s := range m.emptyHints() {
			lines = append(lines, lipgloss.PlaceHorizontal(inner, lipgloss.Center, s))
		}
		return m.st.box(m.listTitle, "", lines, w, h, m.focus == focusList)
	}

	cn, ct, ca, cd := m.columns(inner)
	hdr := fit("   #", cn) + " " + fit("Nomi", ct) + " " + fit("Ijrochi", ca) + " " + lipgloss.PlaceHorizontal(cd, lipgloss.Right, "Vaqt")
	lines = append(lines, m.st.muted.Bold(true).Render(hdr), m.st.subtle.Render(strings.Repeat("─", inner)))

	rows := h - 2 - len(lines)
	// keep cursor in view with a little scroll margin
	margin := min(3, rows/4)
	if m.listCur < m.listOff+margin {
		m.listOff = max(0, m.listCur-margin)
	}
	if m.listCur >= m.listOff+rows-margin {
		m.listOff = m.listCur - rows + margin + 1
	}
	m.listOff = max(0, min(m.listOff, len(m.shown)-rows))

	cur, _ := m.current()
	for r := 0; r < rows && m.listOff+r < len(m.shown); r++ {
		vi := m.listOff + r
		t := m.list[m.shown[vi]]
		playing := t.ID == cur.ID && !m.idle
		liked := m.isLiked(t.ID)
		lines = append(lines, m.trackRow(t, m.shown[vi]+1, playing, liked, vi == m.listCur, m.focus == focusList, inner))
	}

	label := itoa(m.listCur+1) + "/" + itoa(len(m.shown))
	if m.loading != "" {
		label = m.spin() + " " + label
	}
	return m.st.box(m.listTitle, label, lines, w, h, m.focus == focusList)
}

func (m *Model) trackRow(t track.Track, n int, playing, liked, selected, focused bool, inner int) string {
	cn, ct, ca, cd := m.columns(inner)
	num := lipgloss.PlaceHorizontal(cn-1, lipgloss.Right, itoa(n)) + " "
	if playing {
		num = lipgloss.PlaceHorizontal(cn-1, lipgloss.Right, m.eq()) + " "
	}
	heart := "  "
	if liked {
		heart = "♥ "
	}
	dur := lipgloss.PlaceHorizontal(cd-2, lipgloss.Right, track.FormatDur(t.Duration))
	if t.Duration == 0 {
		dur = lipgloss.PlaceHorizontal(cd-2, lipgloss.Right, "—")
	}

	if selected && focused {
		plain := fit(num, cn) + " " + fit(t.Title, ct) + " " + fit(t.Artist, ca) + " " + heart + dur
		st := m.st.sel
		if playing {
			st = st.Foreground(m.st.t.Accent)
		}
		return st.Render(fit(plain, inner))
	}
	ts, as := m.st.fg, m.st.muted
	if playing {
		ts, as = m.st.playing, m.st.accent
	}
	if selected {
		ts = ts.Bold(true).Underline(false)
	}
	numS := m.st.muted.Render(fit(num, cn))
	if playing {
		numS = m.st.accent.Render(fit(num, cn))
	}
	if selected {
		numS = m.st.accent.Render(fit(strings.Replace(num, "  ", " ›", 1), cn))
	}
	return numS + " " + ts.Render(fit(t.Title, ct)) + " " + as.Render(fit(t.Artist, ca)) + " " +
		m.st.bad.Render(heart) + m.st.muted.Render(dur)
}

func (m *Model) emptyHints() []string {
	k := m.st.key.Render
	mu := m.st.muted.Render
	switch m.sources[m.srcCur].kind {
	case srcHistory:
		return []string{mu("Hali hech narsa tinglanmagan"), "", k("/") + mu(" — YouTube'dan qidirish")}
	case srcRadio:
		return []string{mu("Radio — joriy qo'shiqqa o'xshash cheksiz oqim"), "", k("w") + mu(" — tanlangan qo'shiqdan radio")}
	case srcLiked:
		return []string{mu("Yoqtirilgan qo'shiqlar yo'q"), "", k("R") + mu(" — kutubxonani yangilash")}
	}
	return []string{
		m.gradText(logo[0]), m.gradText(logo[1]), "",
		k("/") + mu(" qidirish   ") + k("?") + mu(" yordam   ") + k("tab") + mu(" kutubxona"),
	}
}

func (m *Model) viewQueue(w, h int) string {
	inner := w - 4
	if len(m.queue) == 0 {
		return m.st.box("Navbat", "", []string{"", "", lipgloss.PlaceHorizontal(inner, lipgloss.Center,
			m.st.muted.Render("Navbat bo'sh — ")+m.st.key.Render("a")+m.st.muted.Render(" bilan qo'shing"))}, w, h, m.focus == focusList)
	}
	var total time.Duration
	for _, t := range m.queue[m.qi:] {
		total += t.Duration
	}
	rows := h - 4
	off := max(0, min(m.qCur-rows/3, len(m.queue)-rows))
	lines := []string{
		m.st.muted.Render(fit("Qolgan: "+itoa(len(m.queue)-m.qi-1)+" ta · "+track.FormatDur(total), inner-24)) +
			m.st.muted.Render(lipgloss.PlaceHorizontal(24, lipgloss.Right, "J/K surish · x o'chirish")),
		m.st.subtle.Render(strings.Repeat("─", inner)),
	}
	for r := 0; r < rows && off+r < len(m.queue); r++ {
		i := off + r
		t := m.queue[i]
		row := m.trackRow(t, i+1, i == m.qi && !m.idle, m.isLiked(t.ID), i == m.qCur, m.focus == focusList, inner)
		if i < m.qi && i != m.qCur {
			row = m.st.subtle.Render(fit(lipgloss.PlaceHorizontal(4, lipgloss.Right, itoa(i+1))+"  "+t.Title+" · "+t.Artist, inner))
		}
		lines = append(lines, row)
	}
	label := ""
	if m.shuffle {
		label = "⇄ aralash"
	}
	return m.st.box("Navbat", label, lines, w, h, m.focus == focusList)
}

func (m *Model) viewLyrics(w, h int) string {
	inner := w - 4
	t, ok := m.current()
	title := "Matn"
	if ok {
		title = "Matn · " + t.Title
	}
	center := func(s string) string { return lipgloss.PlaceHorizontal(inner, lipgloss.Center, s) }
	var lines []string
	switch {
	case !ok:
		lines = []string{"", "", center(m.st.muted.Render("Qo'shiq ijro etilmayapti"))}
	case m.lyr == nil && m.lyrErr == "":
		lines = []string{"", "", center(m.st.accent.Render(m.spin()) + m.st.muted.Render(" matn qidirilmoqda…"))}
	case m.lyr == nil:
		lines = []string{"", "", center(m.st.muted.Render(m.lyrErr))}
	default:
		rows := h - 2
		idx := m.lyr.Index(time.Duration(m.pos*float64(time.Second)) + 300*time.Millisecond)
		if !m.lyr.Synced {
			idx = -1
		}
		mid := rows / 2
		start := idx - mid
		if !m.lyr.Synced {
			start = 0
		}
		for r := 0; r < rows; r++ {
			i := start + r
			if i < 0 || i >= len(m.lyr.Lines) {
				lines = append(lines, "")
				continue
			}
			text := m.lyr.Lines[i].Text
			if text == "" {
				text = "♪"
			}
			var s lipgloss.Style
			d := abs(i - idx)
			switch {
			case !m.lyr.Synced:
				s = m.st.fg
			case i == idx:
				s = m.st.accent.Bold(true)
				text = "› " + text + " ‹"
			case d <= 2:
				s = m.st.fg
			case d <= 5:
				s = m.st.muted
			default:
				s = m.st.subtle
			}
			lines = append(lines, center(s.Render(fit(text, min(inner, lipgloss.Width(text))))))
		}
	}
	label := ""
	if m.lyr != nil && m.lyr.Synced {
		label = "sinxron"
	}
	return m.st.box(title, label, lines, w, h, m.focus == focusList)
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func (m *Model) viewPicker(w, h int) string {
	inner := w - 4
	t, _ := m.selectedOrCurrent()
	lines := []string{m.st.muted.Render(fit("Qo'shish: "+t.Title, inner)), m.st.subtle.Render(strings.Repeat("─", inner))}
	rows := h - 4
	off := max(0, m.pickerCur-rows+1)
	for i := off; i < len(m.lib.Playlists) && i-off < rows; i++ {
		p := m.lib.Playlists[i]
		row := fit("  ♫ "+p.Title, inner-6) + lipgloss.PlaceHorizontal(6, lipgloss.Right, itoa(int(p.Count)))
		if i == m.pickerCur {
			row = m.st.sel.Render(row)
		} else {
			row = m.st.fg.Render(row)
		}
		lines = append(lines, row)
	}
	return m.st.box("Playlistga qo'shish", "enter · esc", lines, w, h, true)
}

// ---------- player ----------

func (m *Model) viewPlayer(w, h int) string {
	inner := w - 4
	t, ok := m.current()
	var lines []string

	status := "Hozir ijroda"
	switch {
	case !ok || (m.idle && !m.starting):
		status = "Tayyor"
	case m.starting || m.buffering:
		status = m.spin() + " Yuklanmoqda"
	case m.paused:
		status = "❚❚ Pauza"
	default:
		status = "▶ Ijroda"
	}
	label := ""
	if m.codec != "" && !m.idle {
		label = m.codec
		if m.bitrate > 0 {
			label += " · " + itoa(int(m.bitrate/1000)) + "k"
		}
	}

	vizRows := h - 2 - 5
	if !ok {
		lines = append(lines, m.st.muted.Render("Hech narsa ijro etilmayapti — qo'shiqni tanlab ")+m.st.key.Render("Enter")+m.st.muted.Render(" bosing"))
	} else {
		heart := ""
		if m.isLiked(t.ID) {
			heart = m.st.bad.Render("♥")
		}
		pos := ""
		if len(m.queue) > 1 {
			pos = m.st.muted.Render(itoa(m.qi+1) + "/" + itoa(len(m.queue)) + "  ")
		}
		left := m.st.bold.Render(t.Title) + m.st.muted.Render("  —  ") + m.st.accent.Render(t.Artist)
		right := pos + heart
		lines = append(lines, fit(left, inner-lipgloss.Width(right)-1)+" "+right)
	}
	if vizRows > 0 {
		lines = append(lines, "")
		lines = append(lines, m.viewViz(inner, vizRows)...)
	}
	lines = append(lines, "", m.viewProgress(inner), m.viewControls(inner))
	return m.st.box(status, label, lines, w, h, false)
}

func (m *Model) viewViz(w, rows int) []string {
	bw, gap := 1, 1
	if w >= 140 {
		bw = 2
	}
	n := (w + gap) / (bw + gap)
	playing := !m.paused && !m.idle && !m.starting
	levels := m.an.Bars(n, playing, float64(m.frame)/30)
	out := make([]string, rows)
	eighths := []string{" ", "▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}

	for r := 0; r < rows; r++ {
		var b strings.Builder
		for i, lv := range levels {
			var ch string
			if m.vizMode == 1 { // mirrored around the middle using half blocks
				mid := float64(rows) / 2
				e := math.Max(0.3, lv*float64(rows)/2)
				top := math.Abs(float64(r)+0.25-mid) <= e
				bot := math.Abs(float64(r)+0.75-mid) <= e
				switch {
				case top && bot:
					ch = "█"
				case top:
					ch = "▀"
				case bot:
					ch = "▄"
				default:
					ch = " "
				}
			} else {
				fill := lv*float64(rows*8) - float64((rows-1-r)*8)
				ch = eighths[int(math.Max(0, math.Min(8, fill)))]
				if r == rows-1 && ch == " " {
					ch = "▁"
				}
			}
			col := m.st.gradAt(float64(i) / float64(max(1, n-1)))
			st := lipgloss.NewStyle().Foreground(col)
			if !playing {
				st = m.st.subtle
			}
			b.WriteString(st.Render(strings.Repeat(ch, bw)))
			if i < n-1 {
				b.WriteString(strings.Repeat(" ", gap))
			}
		}
		out[r] = b.String()
	}
	return out
}

func (m *Model) viewProgress(w int) string {
	cur, total := track.FormatDur(time.Duration(m.pos*float64(time.Second))), track.FormatDur(time.Duration(m.dur*float64(time.Second)))
	if m.idle && !m.starting {
		cur, total = "0:00", "0:00"
	}
	bw := w - lipgloss.Width(cur) - lipgloss.Width(total) - 2
	if bw < 5 {
		return ""
	}
	frac := 0.0
	if m.dur > 0 {
		frac = max(0, min(1, m.pos/m.dur))
	}
	filled := int(frac * float64(bw-1))
	var b strings.Builder
	for i := 0; i < filled; i++ {
		b.WriteString(lipgloss.NewStyle().Foreground(m.st.gradAt(float64(i) / float64(bw))).Render("━"))
	}
	knob := "●"
	if m.starting || m.buffering {
		knob = m.spin()
	}
	b.WriteString(m.st.accent2.Bold(true).Render(knob))
	b.WriteString(m.st.subtle.Render(strings.Repeat("─", max(0, bw-filled-1))))
	return m.st.fg.Render(cur) + " " + b.String() + " " + m.st.muted.Render(total)
}

func (m *Model) viewControls(w int) string {
	on := func(b bool, s string) string {
		if b {
			return m.st.accent.Bold(true).Render(s)
		}
		return m.st.muted.Render(s)
	}
	play := "▶"
	if !m.paused && !m.idle {
		play = "❚❚"
	}
	rep := "↻"
	if m.repeat == repeatOne {
		rep = "↻¹"
	}
	ctrl := on(m.shuffle, "⇄") + "    " + m.st.fg.Render("◁◁") + "   " + m.st.accent.Bold(true).Render(play) + "   " +
		m.st.fg.Render("▷▷") + "    " + on(m.repeat != repeatOff, rep)
	if m.cfg.Autoplay {
		ctrl += "    " + m.st.muted.Render("✦ radio")
	}

	vol := int(math.Round(m.volume))
	cells := 10
	fill := min(cells, int(math.Round(m.volume/10)))
	vb := m.st.accent.Render(strings.Repeat("▮", fill)) + m.st.subtle.Render(strings.Repeat("▯", cells-fill))
	icon := "🔊"
	switch {
	case m.muted || vol == 0:
		icon, vb = "🔇", m.st.subtle.Render(strings.Repeat("▯", cells))
	case vol < 35:
		icon = "🔈"
	case vol < 70:
		icon = "🔉"
	}
	right := icon + " " + vb + " " + m.st.fg.Render(lipgloss.PlaceHorizontal(4, lipgloss.Right, itoa(vol)+"%"))
	gap := w - lipgloss.Width(ctrl) - lipgloss.Width(right) - 2
	if gap < 1 {
		return fit("  "+ctrl, w)
	}
	return "  " + ctrl + strings.Repeat(" ", gap) + right
}

// ---------- footer ----------

func (m *Model) viewFooter() string {
	k, mu := m.st.key.Render, m.st.muted.Render
	var hints []string
	add := func(key, desc string) { hints = append(hints, k(key)+" "+mu(desc)) }
	switch {
	case m.searchOn:
		add("enter", "qidirish")
		add("esc", "bekor")
	case m.filterOn:
		add("enter", "tayyor")
		add("esc", "tozalash")
	case m.picker:
		add("j/k", "tanlash")
		add("enter", "qo'shish")
		add("esc", "yopish")
	default:
		add("spc", "pauza")
		add("n/p", "keyingi/oldingi")
		add("←/→", "o'tkazish")
		add("/", "qidirish")
		add("a", "navbatga")
		add("w", "radio")
		add("?", "yordam")
	}
	left := " " + strings.Join(hints, mu("  ·  "))

	right := ""
	if m.toast.text != "" && time.Now().Before(m.toast.expires) {
		st := m.st.accent
		switch m.toast.kind {
		case 1:
			st = m.st.good
		case 2:
			st = m.st.bad
		}
		text := ansi.Truncate(m.toast.text, max(1, m.w*2/3), "…") // never wider than the screen
		right = st.Bold(true).Render(text) + " "
	}
	if right == "" && m.loading != "" { // background work is always visible
		right = m.st.accent.Render(m.spin()+" "+ansi.Truncate(m.loading, max(1, m.w/2), "…")+"…") + " "
	}
	if right != "" {
		lw := m.w - lipgloss.Width(right) - 1
		return fit(left, lw) + " " + right
	}
	return fit(left, m.w)
}

// ---------- help ----------

func (m *Model) viewHelp() string {
	groups := []struct {
		title string
		keys  [][2]string
	}{
		{"Ijro", [][2]string{{"space", "ijro / pauza"}, {"n · p", "keyingi · oldingi"}, {"← →", "±5 soniya"}, {"H · L", "±30 soniya"}, {"+ · -", "ovoz"}, {"m", "ovozsiz"}, {"s", "aralash (shuffle)"}, {"r", "takrorlash rejimi"}, {"z", "uyqu taymeri"}}},
		{"Kutubxona", [][2]string{{"j k · ↑ ↓", "harakat"}, {"g · G", "boshi · oxiri"}, {"tab · h l", "panel almashtirish"}, {"enter", "ijro etish"}, {"/", "YouTube qidiruv / havola"}, {"f", "ro'yxatni filtrlash"}, {"1 2 3", "qo'shiqlar · navbat · matn"}, {"R", "kutubxonani yangilash"}}},
		{"Navbat va radio", [][2]string{{"a", "navbatga qo'shish"}, {"e", "keyingi bo'lib qo'yish"}, {"x", "navbatdan olib tashlash"}, {"J · K", "navbatda surish"}, {"w", "qo'shiqdan radio"}}},
		{"Akkaunt va boshqa", [][2]string{{"A", "akkauntga kirish / chiqish"}, {"F · *", "yoqtirish ♥"}, {"b", "playlistga qo'shish"}, {"o", "brauzerda ochish"}, {"c", "havolani nusxalash"}, {"t", "mavzu"}, {"v", "vizualizator rejimi"}, {"q", "chiqish"}}},
	}
	const colW = 40
	var cols []string
	for _, g := range groups {
		var b []string
		b = append(b, m.st.accent.Bold(true).Render(g.title))
		for _, kv := range g.keys {
			b = append(b, m.st.key.Render(fit(kv[0], 11))+" "+m.st.fg.Render(kv[1]))
		}
		cols = append(cols, padR(strings.Join(b, "\n"), colW))
	}
	var grid string
	switch {
	case m.w >= 4*colW+8:
		grid = lipgloss.JoinHorizontal(lipgloss.Top, cols...)
	case m.w >= 2*colW+8:
		grid = lipgloss.JoinVertical(lipgloss.Left,
			lipgloss.JoinHorizontal(lipgloss.Top, cols[0], cols[1]), "",
			lipgloss.JoinHorizontal(lipgloss.Top, cols[2], cols[3]))
	default:
		grid = lipgloss.JoinVertical(lipgloss.Left, cols[0], "", cols[1], "", cols[2], "", cols[3])
	}
	lines := []string{m.gradText(logo[0]), m.gradText(logo[1]), "", m.st.muted.Render("YouTube musiqa pleyeri · terminal uchun"), ""}
	lines = append(lines, strings.Split(grid, "\n")...)
	lines = append(lines, "", m.st.muted.Render("MPRIS: media tugmalari va playerctl bilan ishlaydi"))
	return m.card("Yordam", m.st.muted.Render("? · esc yopish"), lines, true, &m.helpOff)
}

func padR(s string, w int) string { return lipgloss.NewStyle().Width(w).Render(s) }

// card centers a bordered panel that always fits the terminal: blank lines
// and then top lines are dropped when short on rows, or — with scroll set —
// the content scrolls (j/k) instead.
func (m *Model) card(title, label string, lines []string, focused bool, scroll *int) string {
	var flat []string
	for _, l := range lines {
		flat = append(flat, strings.Split(l, "\n")...)
	}
	cw := 0
	for _, l := range flat {
		cw = max(cw, lipgloss.Width(l))
	}
	bw := min(m.w, cw+4+8) // borders+gutter, plus breathing room
	inner := bw - 4
	rows := m.h - 2
	if bw < 8 || rows < 1 {
		return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, "")
	}
	if len(flat)+2 <= rows { // roomy: add vertical padding
		flat = append(append([]string{""}, flat...), "")
	}
	if len(flat) > rows && scroll == nil {
		var keep []string // drop blank lines first
		extra := len(flat) - rows
		for _, l := range flat {
			if extra > 0 && strings.TrimSpace(ansiStrip(l)) == "" {
				extra--
				continue
			}
			keep = append(keep, l)
		}
		flat = keep
		if len(flat) > rows {
			flat = flat[len(flat)-rows:]
		}
	}
	if scroll != nil {
		*scroll = max(0, min(*scroll, len(flat)-rows))
		if len(flat) > rows {
			flat = flat[*scroll : *scroll+rows]
			label = m.st.muted.Render("j/k ↕ · ") + label
		}
	}
	out := make([]string, len(flat))
	for i, l := range flat {
		out[i] = lipgloss.PlaceHorizontal(inner, lipgloss.Center, ansi.Truncate(l, inner, "…"))
	}
	box := m.st.box(title, label, out, bw, len(out)+2, focused)
	return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, box)
}

func ansiStrip(s string) string { return ansi.Strip(s) }

func spaced(s string) string { return strings.Join(strings.Split(s, ""), " ") }

func nz(s, d string) string {
	if s == "" {
		return d
	}
	return s
}
