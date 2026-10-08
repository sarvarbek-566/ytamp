// Package config handles ytamp's on-disk configuration and XDG paths.
package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config is the user-editable configuration (~/.config/ytamp/config.toml).
type Config struct {
	// Google OAuth "TV and Limited Input devices" client credentials.
	ClientID     string `toml:"client_id"`
	ClientSecret string `toml:"client_secret"`

	Theme         string `toml:"theme"`          // omarchy | tokyonight | catppuccin | nord | gruvbox | rosepine
	Volume        int    `toml:"volume"`         // startup volume 0-100
	AudioQuality  string `toml:"audio_quality"`  // best | medium | low
	Notifications bool   `toml:"notifications"`  // desktop notification on track change
	Autoplay      bool   `toml:"autoplay"`       // radio mode when queue ends
	Visualizer    string `toml:"visualizer"`     // bars | wave | off
	ResumeSession bool   `toml:"resume_session"` // restore queue/position on start
	SearchResults int    `toml:"search_results"`
	AudioDevice   string `toml:"audio_device"` // mpv --audio-device, e.g. pipewire/<sink>
	// Browser whose YouTube cookies yt-dlp may use when YouTube asks to
	// "confirm you're not a bot" (firefox, chromium, brave…). Empty = off.
	CookiesFromBrowser string `toml:"cookies_from_browser"`
}

func defaults() Config {
	return Config{
		Theme:         "omarchy",
		Volume:        70,
		AudioQuality:  "best",
		Notifications: true,
		Autoplay:      true,
		Visualizer:    "bars",
		ResumeSession: true,
		SearchResults: 30,
	}
}

// ConfigDir returns ~/.config/ytamp.
func ConfigDir() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "ytamp")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".config", "ytamp")
}

// StateDir returns ~/.local/state/ytamp (token, session, cache).
func StateDir() string {
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return filepath.Join(d, "ytamp")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".local", "state", "ytamp")
}

// RuntimeDir returns a per-user runtime dir for the mpv socket.
func RuntimeDir() string {
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		return filepath.Join(d, "ytamp")
	}
	return filepath.Join(os.TempDir(), "ytamp-"+strconv.Itoa(os.Getuid()))
}

// Path of the config file.
func Path() string { return filepath.Join(ConfigDir(), "config.toml") }

// Load reads the config, writing a commented default file on first run.
func Load() (Config, error) {
	c := defaults()
	_, err := toml.DecodeFile(Path(), &c)
	if errors.Is(err, fs.ErrNotExist) {
		return c, writeDefault()
	}
	if c.Volume < 0 || c.Volume > 100 {
		c.Volume = 70
	}
	if c.SearchResults <= 0 || c.SearchResults > 100 {
		c.SearchResults = 30
	}
	return c, err
}

func writeDefault() error {
	if err := os.MkdirAll(ConfigDir(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(Path(), []byte(defaultFile), 0o600)
}

const defaultFile = `# ytamp — terminal YouTube music player
#
# YouTube akkaunt bilan kirish uchun Google Cloud'da OAuth client yarating:
#   1. https://console.cloud.google.com → yangi loyiha
#   2. "YouTube Data API v3" ni yoqing
#   3. Credentials → Create credentials → OAuth client ID
#      Application type: "TVs and Limited Input devices"
#   4. Client ID va secret'ni quyiga yozing
client_id     = ""
client_secret = ""

# Mavzu: omarchy (joriy Omarchy temasidan), tokyonight, catppuccin, nord, gruvbox, rosepine
theme = "omarchy"

volume         = 70
audio_quality  = "best"   # best | medium | low
notifications  = true     # trek almashganda desktop xabar
autoplay       = true     # navbat tugasa o'xshash qo'shiqlar (radio)
visualizer     = "bars"   # bars | wave | off
resume_session = true     # oxirgi navbat va pozitsiyani tiklash
search_results = 30

# Chiqish qurilmasi (bo'sh = tizim standarti). Ro'yxat: mpv --audio-device=help
audio_device = ""

# YouTube "bot emasligingizni tasdiqlang" desa: brauzeringizdagi YouTube
# sessiyasidan foydalanish (firefox, chromium, brave …). Bo'sh = o'chiq.
cookies_from_browser = ""
`

// Set writes key = "value" into config.toml, replacing an existing line for
// the key (comments and other settings are kept) or appending it.
func Set(key, value string) error {
	if _, err := os.Stat(Path()); errors.Is(err, fs.ErrNotExist) {
		if err := writeDefault(); err != nil {
			return err
		}
	}
	b, err := os.ReadFile(Path())
	if err != nil {
		return err
	}
	line := key + " = " + strconv.Quote(value)
	lines := strings.Split(string(b), "\n")
	done := false
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "#") {
			continue
		}
		if k, _, ok := strings.Cut(t, "="); ok && strings.TrimSpace(k) == key {
			lines[i], done = line, true
			break
		}
	}
	if !done {
		if n := len(lines); n > 0 && lines[n-1] == "" {
			lines = append(lines[:n-1], line, "")
		} else {
			lines = append(lines, line)
		}
	}
	tmp := Path() + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, Path())
}
