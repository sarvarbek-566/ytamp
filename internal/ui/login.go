package ui

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/voidsecurity404/ytamp/internal/auth"
	"github.com/voidsecurity404/ytamp/internal/config"
	"github.com/voidsecurity404/ytamp/internal/store"
	"github.com/voidsecurity404/ytamp/internal/track"
	"github.com/voidsecurity404/ytamp/internal/ytdl"
)

// Login flow:
//
//	choose ─┬─ browser  → checking the browser's YouTube session → main
//	        ├─ oauth    → client id/secret form → device code → main
//	        ├─ guest / back
//	        └─ logout (when signed in)
type loginStage int

const (
	stageChoose loginStage = iota
	stageBrowser
	stageOAuthForm
	stageDevice
)

const credentialsURL = "https://console.cloud.google.com/apis/credentials"

type loginOption struct {
	kind    string // browser | oauth | guest | logout
	browser ytdl.Browser
	title   string
	hint    string
}

// Swappable for tests.
var (
	detectBrowsers   = ytdl.DetectBrowsers
	newBrowserClient = func(b string) ytAPI { return ytdl.NewBrowserClient(b) }
	saveConfig       = config.Set
	deviceStart      = auth.Start
	deviceWait       = auth.Wait
)

type browserMsg struct {
	browser string
	yt      ytAPI
	user    string
	err     error
}

// openLogin shows the sign-in chooser; fromMain means esc returns to the
// player instead of continuing as a guest.
func (m *Model) openLogin(fromMain bool) {
	m.scr, m.stage = screenLogin, stageChoose
	m.loginReturn = fromMain
	m.loginErr, m.loginCur = "", 0
	m.browsers = detectBrowsers()
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
}

func (m *Model) loginOptions() []loginOption {
	var o []loginOption
	for _, b := range m.browsers {
		o = append(o, loginOption{kind: "browser", browser: b, title: b.Label + " orqali",
			hint: "brauzerdagi YouTube sessiyangiz · sozlash shart emas"})
	}
	o = append(o, loginOption{kind: "oauth", title: "Google OAuth",
		hint: "o'z Google Cloud client ID'ingiz bilan"})
	if m.yt != nil {
		o = append(o, loginOption{kind: "logout", title: "Akkauntdan chiqish", hint: m.user})
	}
	back := "Mehmon rejimi"
	if m.loginReturn {
		back = "Orqaga"
	}
	return append(o, loginOption{kind: "guest", title: back, hint: "akkauntsiz: qidiruv, radio, tarix"})
}

func (m *Model) leaveLogin() {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.enterMain()
}

func (m *Model) onLoginKey(k tea.KeyMsg) tea.Cmd {
	key := k.String()
	switch m.stage {
	case stageChoose:
		opts := m.loginOptions()
		switch key {
		case "q":
			m.quitting = true
			return tea.Quit
		case "esc", "g":
			m.leaveLogin()
		case "j", "down", "tab":
			m.loginCur = (m.loginCur + 1) % len(opts)
		case "k", "up", "shift+tab":
			m.loginCur = (m.loginCur - 1 + len(opts)) % len(opts)
		case "enter":
			return m.chooseLogin(opts[max(0, min(m.loginCur, len(opts)-1))])
		}

	case stageBrowser:
		switch key {
		case "esc":
			m.stage, m.loginErr = stageChoose, ""
		case "r", "enter":
			if m.loginErr != "" {
				return m.tryBrowser(m.tryingBrowser)
			}
		}

	case stageOAuthForm:
		switch key {
		case "esc":
			m.stage, m.loginErr = stageChoose, ""
			m.idIn.Blur()
			m.secIn.Blur()
		case "ctrl+o":
			openURL(credentialsURL)
		case "tab", "shift+tab", "up", "down":
			m.formFocus = 1 - m.formFocus
			return m.focusForm()
		case "enter":
			if m.formFocus == 0 {
				m.formFocus = 1
				return m.focusForm()
			}
			return m.submitOAuth()
		default:
			var cmd tea.Cmd
			if m.formFocus == 0 {
				m.idIn, cmd = m.idIn.Update(k)
			} else {
				m.secIn, cmd = m.secIn.Update(k)
			}
			return cmd
		}

	case stageDevice:
		switch key {
		case "esc": // back to the form, so wrong credentials can be fixed
			if m.cancel != nil {
				m.cancel()
				m.cancel = nil
			}
			m.stage, m.da, m.loginErr = stageOAuthForm, nil, ""
			return m.focusForm()
		case "o":
			if m.da != nil {
				openURL(m.da.VerificationURI)
			}
		case "c":
			if m.da != nil {
				copyText(m.da.UserCode)
				m.flash("Kod nusxalandi", 1)
			}
		case "r":
			if m.da == nil || m.loginErr != "" {
				m.da, m.loginErr = nil, ""
				return m.startDevice()
			}
		}
	}
	return nil
}

func (m *Model) chooseLogin(o loginOption) tea.Cmd {
	switch o.kind {
	case "browser":
		return m.tryBrowser(o.browser)
	case "oauth":
		m.stage, m.loginErr = stageOAuthForm, ""
		m.idIn = newField("Client ID", m.cfg.ClientID, false)
		m.secIn = newField("Client secret", m.cfg.ClientSecret, true)
		m.formFocus = 0
		if m.cfg.ClientID != "" {
			m.formFocus = 1
		}
		return m.focusForm()
	case "logout":
		m.logout()
		m.leaveLogin()
		m.flash("Akkauntdan chiqildi", 0)
	default:
		m.leaveLogin()
	}
	return nil
}

func newField(placeholder, value string, secret bool) textinput.Model {
	in := textinput.New()
	in.Placeholder = placeholder
	in.Prompt = ""
	in.CharLimit = 200
	in.Width = 44
	in.SetValue(value)
	if secret {
		in.EchoMode = textinput.EchoPassword
		in.EchoCharacter = '•'
	}
	return in
}

func (m *Model) focusForm() tea.Cmd {
	if m.formFocus == 0 {
		m.secIn.Blur()
		return m.idIn.Focus()
	}
	m.idIn.Blur()
	return m.secIn.Focus()
}

func (m *Model) submitOAuth() tea.Cmd {
	id, sec := strings.TrimSpace(m.idIn.Value()), strings.TrimSpace(m.secIn.Value())
	switch {
	case id == "" || sec == "":
		m.loginErr = "Client ID va secret ikkalasi ham kerak"
		return nil
	case !strings.HasSuffix(id, ".apps.googleusercontent.com"):
		m.loginErr = "Client ID odatda …apps.googleusercontent.com bilan tugaydi"
		return nil
	}
	cfg := m.cfg
	cfg.ClientID, cfg.ClientSecret = id, sec
	oc, err := auth.OAuthConfig(cfg)
	if err != nil {
		m.loginErr = err.Error()
		return nil
	}
	// saved only once Google accepts them (see onDevice)
	m.oc, m.pendingCreds = oc, true
	m.stage, m.da, m.loginErr = stageDevice, nil, ""
	return m.startDevice()
}

func (m *Model) tryBrowser(b ytdl.Browser) tea.Cmd {
	m.stage, m.tryingBrowser, m.loginErr = stageBrowser, b, ""
	return m.connectBrowser(b.Name)
}

func (m *Model) connectBrowser(name string) tea.Cmd {
	newBrowserClient := newBrowserClient
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		c := newBrowserClient(name)
		user, err := c.Me(ctx)
		if err != nil {
			return browserMsg{browser: name, err: err}
		}
		return browserMsg{browser: name, yt: c, user: user}
	}
}

// onBrowser handles the result of checking a browser session, both from the
// login screen and from a remembered browser at startup.
func (m *Model) onBrowser(msg browserMsg) tea.Cmd {
	fromLogin := m.scr == screenLogin && m.stage == stageBrowser && m.tryingBrowser.Name == msg.browser
	if msg.err != nil {
		if fromLogin {
			m.loginErr = errText(msg.err)
		} else if m.connecting {
			m.connecting = false
			m.flash("Brauzer sessiyasi: "+errText(msg.err)+" · A — qayta kirish", 2)
		}
		return nil
	}
	if !fromLogin && !m.connecting {
		return nil // stale: the user moved on
	}
	if fromLogin {
		if err := saveConfig("cookies_from_browser", msg.browser); err != nil {
			m.loginErr = "config'ni yozib bo'lmadi: " + err.Error()
			return nil
		}
		m.cfg.CookiesFromBrowser = msg.browser
		ytdl.CookiesFromBrowser = msg.browser
		m.leaveLogin()
		m.flash("✓ Kirildi: "+msg.user, 1)
	}
	return m.signedIn(msg.yt, msg.user)
}

// signedIn switches the library to an authenticated account.
func (m *Model) signedIn(yt ytAPI, user string) tea.Cmd {
	m.connecting = false
	if m.lib.User != "" && m.lib.User != user { // another account: drop its cache
		m.lib = store.Library{Tracks: map[string][]track.Track{}}
		store.ClearLibrary()
	}
	m.yt, m.user = yt, user
	m.lib.User = user
	m.rebuildSources()
	if len(m.lib.Liked) == 0 || time.Since(m.lib.Updated) > 6*time.Hour {
		if len(m.lib.Liked) == 0 {
			m.loading = "Kutubxona yuklanmoqda"
		}
		return m.fetchLibrary()
	}
	if m.srcCur == 0 || len(m.list) == 0 {
		m.selectSource(0, false)
	}
	return nil
}

// logout forgets every credential and the cached library.
func (m *Model) logout() {
	_ = auth.Logout()
	if m.cfg.CookiesFromBrowser != "" {
		_ = saveConfig("cookies_from_browser", "")
	}
	m.cfg.CookiesFromBrowser = ""
	ytdl.CookiesFromBrowser = ""
	store.ClearLibrary()
	m.yt, m.user, m.connecting = nil, "", false
	m.lib = store.Library{Tracks: map[string][]track.Track{}}
	m.srcCur = 0
	m.list, m.shown = nil, nil
	m.rebuildSources()
}

// ---------- view ----------

func (m *Model) viewLogin() string {
	k, mu, fg := m.st.key.Render, m.st.muted.Render, m.st.fg.Render
	head := []string{m.gradText(logo[0]), m.gradText(logo[1]), "", mu("YouTube musiqa pleyeri · terminal uchun"), ""}
	var body []string
	const rowW = 58

	switch m.stage {
	case stageChoose:
		body = append(body, fg("YouTube akkauntingizga qanday kiramiz?"), "")
		for i, o := range m.loginOptions() {
			label := o.title
			if o.kind == "browser" && i == 0 {
				label += "  (tavsiya)"
			}
			if i == m.loginCur { // the hint is shown for the highlighted choice only
				body = append(body, m.st.sel.Render(fit("▸ "+label, rowW)), m.st.accent.Render(fit("    "+o.hint, rowW)))
			} else {
				body = append(body, fg(fit("  "+label, rowW)))
			}
		}
		if len(m.browsers) == 0 {
			body = append(body, "", m.st.warn.Render("Brauzer profili topilmadi — Google OAuth'dan foydalaning"))
		}
		body = append(body, "", k("↑↓")+mu(" tanlash   ")+k("enter")+mu(" davom   ")+k("esc")+mu(" orqaga   ")+k("q")+mu(" chiqish"))

	case stageBrowser:
		b := m.tryingBrowser.Label
		if m.loginErr == "" {
			body = append(body, m.st.accent.Render(m.spin())+fg(" "+b+" sessiyasi tekshirilmoqda…"), "",
				mu("Brauzer cookie'lari kompyuterdan faqat YouTube'ga yuboriladi"))
		} else {
			body = append(body, m.st.bad.Render("✗ "+m.loginErr), "",
				mu("1. "+b+"'da youtube.com'ni ochib akkauntga kiring"),
				mu("2. Brauzerni yopmasdan qayta urinib ko'ring"), "",
				k("r")+mu(" qayta urinish   ")+k("esc")+mu(" boshqa usul"))
		}

	case stageOAuthForm:
		field := func(label string, in textinput.Model, focused bool) string {
			st := m.st.subtle
			if focused {
				st = m.st.accent
			}
			return st.Render(fit(label, 15)) + " " + in.View()
		}
		body = append(body, fg("Google OAuth client"), "",
			mu("console.cloud.google.com → YouTube Data API v3 ni yoqing →"),
			mu("Credentials → OAuth client ID → \"TVs and Limited Input\""), "",
			field("Client ID", m.idIn, m.formFocus == 0),
			field("Client secret", m.secIn, m.formFocus == 1), "")
		if m.loginErr != "" {
			body = append(body, m.st.bad.Render("✗ "+m.loginErr), "")
		}
		body = append(body, k("tab")+mu(" maydon   ")+k("enter")+mu(" saqlash   ")+k("ctrl+o")+mu(" Cloud Console   ")+k("esc")+mu(" orqaga"))

	case stageDevice:
		switch {
		case m.da == nil && m.loginErr == "":
			body = append(body, m.st.accent.Render(m.spin())+fg(" Google bilan bog'lanilmoqda…"))
		case m.da == nil:
			body = append(body, m.st.bad.Render("✗ "+m.loginErr), "", k("r")+mu(" qayta urinish   ")+k("esc")+mu(" orqaga"))
		default:
			code := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(m.st.t.Accent).
				Padding(0, 3).Foreground(m.st.t.Accent).Bold(true).Render(spaced(m.da.UserCode))
			left := time.Until(m.da.Expiry).Round(time.Second)
			body = append(body,
				fg("YouTube akkauntingiz bilan kiring"), "",
				mu("1. Telefon yoki brauzerda oching"),
				m.st.accent.Underline(true).Render(m.da.VerificationURI), "",
				mu("2. Shu kodni kiriting"), code, "")
			if m.loginErr != "" {
				body = append(body, m.st.bad.Render("✗ "+m.loginErr), k("r")+mu(" yangi kod"))
			} else {
				body = append(body, m.st.accent.Render(m.spin())+mu(" Tasdiqlash kutilmoqda… ")+m.st.subtle.Render(track.FormatDur(max(0, left))))
			}
			body = append(body, "", k("o")+mu(" havolani ochish   ")+k("c")+mu(" kodni nusxalash   ")+k("esc")+mu(" orqaga"))
		}
	}
	return m.card("Akkaunt", "", append(head, body...), true, nil)
}

// onDevice handles Google's answer to the device-code request.
func (m *Model) onDevice(msg deviceMsg) tea.Cmd {
	if m.scr != screenLogin || m.stage != stageDevice { // user went back
		return nil
	}
	if msg.err != nil {
		m.loginErr = errText(msg.err)
		return nil
	}
	if m.pendingCreds { // Google knows this client: remember it
		for k, v := range map[string]string{"client_id": m.oc.ClientID, "client_secret": m.oc.ClientSecret} {
			if err := saveConfig(k, v); err != nil {
				m.loginErr = "config'ni yozib bo'lmadi: " + err.Error()
				return nil
			}
		}
		m.cfg.ClientID, m.cfg.ClientSecret = m.oc.ClientID, m.oc.ClientSecret
		m.pendingCreds = false
	}
	m.da, m.loginErr = msg.da, ""
	return m.waitToken()
}
