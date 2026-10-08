package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/sarvarbek-566/ytamp/internal/theme"
)

// styles is the precomputed style set for the active theme.
type styles struct {
	t       theme.Theme
	fg      lipgloss.Style
	muted   lipgloss.Style
	subtle  lipgloss.Style
	accent  lipgloss.Style
	accent2 lipgloss.Style
	bold    lipgloss.Style
	sel     lipgloss.Style
	selDim  lipgloss.Style
	playing lipgloss.Style
	good    lipgloss.Style
	warn    lipgloss.Style
	bad     lipgloss.Style
	key     lipgloss.Style
	grad    []lipgloss.Color
}

func newStyles(t theme.Theme) styles {
	s := lipgloss.NewStyle
	st := styles{
		t:       t,
		fg:      s().Foreground(t.Fg),
		muted:   s().Foreground(t.Muted),
		subtle:  s().Foreground(t.Subtle),
		accent:  s().Foreground(t.Accent),
		accent2: s().Foreground(t.Accent2),
		bold:    s().Foreground(t.Fg).Bold(true),
		sel:     s().Foreground(t.Fg).Background(t.Select).Bold(true),
		selDim:  s().Foreground(t.Fg).Background(t.Select),
		playing: s().Foreground(t.Accent).Bold(true),
		good:    s().Foreground(t.Good),
		warn:    s().Foreground(t.Warn),
		bad:     s().Foreground(t.Bad),
		key:     s().Foreground(t.Accent).Bold(true),
	}
	st.grad = gradient(t.Grad, 32)
	return st
}

// gradient interpolates n colors across the given hex stops.
func gradient(stops []string, n int) []lipgloss.Color {
	if len(stops) == 0 {
		stops = []string{"#7aa2f7"}
	}
	if len(stops) == 1 {
		stops = append(stops, stops[0])
	}
	out := make([]lipgloss.Color, n)
	for i := 0; i < n; i++ {
		f := float64(i) / float64(max(n-1, 1)) * float64(len(stops)-1)
		k := min(int(f), len(stops)-2)
		out[i] = lipgloss.Color(mix(stops[k], stops[k+1], f-float64(k)))
	}
	return out
}

func hexRGB(h string) (r, g, b int) {
	h = strings.TrimPrefix(h, "#")
	if len(h) != 6 {
		return 128, 128, 128
	}
	v, _ := strconv.ParseUint(h, 16, 32)
	return int(v >> 16 & 255), int(v >> 8 & 255), int(v & 255)
}

func mix(a, b string, f float64) string {
	r1, g1, b1 := hexRGB(a)
	r2, g2, b2 := hexRGB(b)
	l := func(x, y int) int { return x + int(float64(y-x)*f) }
	return fmt.Sprintf("#%02x%02x%02x", l(r1, r2), l(g1, g2), l(b1, b2))
}

// gradAt picks a gradient color for position f in 0..1.
func (s styles) gradAt(f float64) lipgloss.Color {
	i := int(f * float64(len(s.grad)-1))
	return s.grad[max(0, min(len(s.grad)-1, i))]
}

// fit truncates (ANSI-aware) to w cells and pads with spaces to exactly w.
func fit(str string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(str) > w {
		str = ansi.Truncate(str, w, "…")
	}
	return str + strings.Repeat(" ", max(0, w-lipgloss.Width(str)))
}

// box draws a rounded panel with an inline title and optional right label.
func (s styles) box(title, label string, lines []string, w, h int, focused bool) string {
	if w < 6 || h < 2 {
		return ""
	}
	bc := s.subtle
	tc := s.muted
	if focused {
		bc = s.accent
		tc = s.accent.Bold(true)
	}
	inner := w - 4
	top := bc.Render("╭─")
	used := 2
	if title != "" {
		t := " " + ansi.Truncate(title, w-8, "…") + " "
		top += tc.Render(t)
		used += lipgloss.Width(t)
	}
	lab := ""
	if label != "" {
		lab = " " + label + " "
	}
	fill := w - used - 1 // room left before the closing corner
	if lab != "" {
		fill -= lipgloss.Width(lab) + 1
	}
	if fill < 0 {
		lab, fill = "", w-used-1
	}
	top += bc.Render(strings.Repeat("─", max(0, fill)))
	if lab != "" {
		top += s.muted.Render(lab) + bc.Render("─")
	}
	top += bc.Render("╮")

	var b strings.Builder
	b.WriteString(top)
	side := bc.Render("│")
	for i := 0; i < h-2; i++ {
		ln := ""
		if i < len(lines) {
			ln = lines[i]
		}
		b.WriteString("\n" + side + " " + fit(ln, inner) + " " + side)
	}
	b.WriteString("\n" + bc.Render("╰"+strings.Repeat("─", w-2)+"╯"))
	return b.String()
}
