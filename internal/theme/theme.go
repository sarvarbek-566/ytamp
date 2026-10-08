// Package theme provides color palettes, including one read live from Omarchy.
package theme

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/charmbracelet/lipgloss"
)

// Theme is a semantic palette used by the UI.
type Theme struct {
	Name    string
	Bg      lipgloss.Color // panel background (may be empty = terminal bg)
	Fg      lipgloss.Color
	Muted   lipgloss.Color
	Subtle  lipgloss.Color // borders
	Accent  lipgloss.Color
	Accent2 lipgloss.Color
	Select  lipgloss.Color // selection background
	Good    lipgloss.Color
	Warn    lipgloss.Color
	Bad     lipgloss.Color
	Grad    []string // visualizer gradient, low → high
}

// Names lists the selectable themes in cycle order.
var Names = []string{"omarchy", "tokyonight", "catppuccin", "nord", "gruvbox", "rosepine"}

var builtin = map[string]Theme{
	"tokyonight": {Name: "tokyonight", Fg: "#c0caf5", Muted: "#565f89", Subtle: "#3b4261", Accent: "#7aa2f7",
		Accent2: "#bb9af7", Select: "#283457", Good: "#9ece6a", Warn: "#e0af68", Bad: "#f7768e",
		Grad: []string{"#7dcfff", "#7aa2f7", "#bb9af7", "#f7768e"}},
	"catppuccin": {Name: "catppuccin", Fg: "#cdd6f4", Muted: "#6c7086", Subtle: "#45475a", Accent: "#cba6f7",
		Accent2: "#f5c2e7", Select: "#313244", Good: "#a6e3a1", Warn: "#f9e2af", Bad: "#f38ba8",
		Grad: []string{"#89dceb", "#89b4fa", "#cba6f7", "#f5c2e7"}},
	"nord": {Name: "nord", Fg: "#d8dee9", Muted: "#4c566a", Subtle: "#3b4252", Accent: "#88c0d0",
		Accent2: "#81a1c1", Select: "#434c5e", Good: "#a3be8c", Warn: "#ebcb8b", Bad: "#bf616a",
		Grad: []string{"#8fbcbb", "#88c0d0", "#81a1c1", "#b48ead"}},
	"gruvbox": {Name: "gruvbox", Fg: "#ebdbb2", Muted: "#928374", Subtle: "#504945", Accent: "#fabd2f",
		Accent2: "#fe8019", Select: "#3c3836", Good: "#b8bb26", Warn: "#fabd2f", Bad: "#fb4934",
		Grad: []string{"#b8bb26", "#fabd2f", "#fe8019", "#fb4934"}},
	"rosepine": {Name: "rosepine", Fg: "#e0def4", Muted: "#6e6a86", Subtle: "#403d52", Accent: "#ebbcba",
		Accent2: "#c4a7e7", Select: "#2a283e", Good: "#9ccfd8", Warn: "#f6c177", Bad: "#eb6f92",
		Grad: []string{"#31748f", "#9ccfd8", "#c4a7e7", "#ebbcba"}},
}

// Get returns a theme by name; "omarchy" reads the active Omarchy palette and
// falls back to tokyonight if it can't be found.
func Get(name string) Theme {
	if name == "omarchy" {
		if t, ok := omarchy(); ok {
			return t
		}
		t := builtin["tokyonight"]
		t.Name = "omarchy"
		return t
	}
	if t, ok := builtin[name]; ok {
		return t
	}
	return builtin["tokyonight"]
}

// Next returns the theme name after cur in the cycle.
func Next(cur string) string {
	for i, n := range Names {
		if n == cur {
			return Names[(i+1)%len(Names)]
		}
	}
	return Names[0]
}

func omarchy() (Theme, bool) {
	h, _ := os.UserHomeDir()
	paths := []string{
		filepath.Join(h, ".local/state/omarchy/current/theme/colors.toml"),
		filepath.Join(h, ".config/omarchy/current/theme/colors.toml"),
	}
	for _, p := range paths {
		var c struct {
			Accent            string `toml:"accent"`
			Selection         string `toml:"selection"`
			Muted             string `toml:"muted"`
			Foreground        string `toml:"foreground"`
			LighterBackground string `toml:"lighter_background"`
			Red               string `toml:"red"`
			Yellow            string `toml:"yellow"`
			Green             string `toml:"green"`
			Cyan              string `toml:"cyan"`
			Blue              string `toml:"blue"`
			Magenta           string `toml:"magenta"`
		}
		if _, err := toml.DecodeFile(p, &c); err != nil || c.Accent == "" {
			continue
		}
		or := func(v, d string) lipgloss.Color {
			if v == "" {
				return lipgloss.Color(d)
			}
			return lipgloss.Color(v)
		}
		return Theme{
			Name: "omarchy", Fg: or(c.Foreground, "#c0caf5"), Muted: or(c.Muted, "#565f89"),
			Subtle: or(c.LighterBackground, "#3b4261"), Accent: or(c.Accent, "#7aa2f7"),
			Accent2: or(c.Magenta, "#bb9af7"), Select: or(c.Selection, "#283457"),
			Good: or(c.Green, "#9ece6a"), Warn: or(c.Yellow, "#e0af68"), Bad: or(c.Red, "#f7768e"),
			Grad: []string{nz(c.Cyan, "#7dcfff"), nz(c.Blue, "#7aa2f7"), nz(c.Magenta, "#bb9af7"), nz(c.Red, "#f7768e")},
		}, true
	}
	return Theme{}, false
}

func nz(v, d string) string {
	if v == "" {
		return d
	}
	return v
}
