// Package track defines the Track model shared by every ytamp component.
package track

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Track is one playable YouTube video.
type Track struct {
	ID       string        `json:"id"`
	Title    string        `json:"title"`
	Artist   string        `json:"artist"`
	Duration time.Duration `json:"duration"`
	Liked    bool          `json:"liked,omitempty"`
}

// URL is the canonical watch URL.
func (t Track) URL() string { return "https://www.youtube.com/watch?v=" + t.ID }

// Thumb is the hq thumbnail URL.
func (t Track) Thumb() string { return "https://i.ytimg.com/vi/" + t.ID + "/hqdefault.jpg" }

var (
	noise = regexp.MustCompile(`(?i)\s*[\(\[][^\)\]]*(official|video|audio|lyric|lyrics|visuali[sz]er|hd|4k|mv|m/v|clip|remaster(ed)?|music)[^\)\]]*[\)\]]`)
	feat  = regexp.MustCompile(`(?i)\s+(ft\.?|feat\.?|featuring)\s+.*$`)
	topic = regexp.MustCompile(`(?i)\s*-\s*topic$`)
	space = regexp.MustCompile(`\s+`)
)

// Clean splits a raw YouTube title/channel into a tidy (artist, title).
// "The Weeknd - Starboy (Official Video) ft. Daft Punk" / "TheWeekndVEVO"
// → ("The Weeknd", "Starboy").
func Clean(rawTitle, channel string) (artist, title string) {
	t := noise.ReplaceAllString(rawTitle, "")
	t = strings.NewReplacer("“", "", "”", "", "\"", "", "|", " ").Replace(t)
	t = strings.TrimSpace(space.ReplaceAllString(t, " "))

	artist = topic.ReplaceAllString(channel, "")
	artist = strings.TrimSuffix(artist, "VEVO")
	artist = strings.TrimSuffix(artist, "Official")
	artist = strings.TrimSpace(artist)

	for _, sep := range []string{" - ", " – ", " — ", " ~ "} {
		if a, b, ok := strings.Cut(t, sep); ok {
			artist, t = strings.TrimSpace(a), strings.TrimSpace(b)
			break
		}
	}
	t = strings.TrimSpace(feat.ReplaceAllString(t, ""))
	if t == "" {
		t = rawTitle
	}
	return artist, t
}

// FormatDur renders a duration as m:ss or h:mm:ss.
func FormatDur(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	s := int(d.Round(time.Second).Seconds())
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s%3600/60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}
