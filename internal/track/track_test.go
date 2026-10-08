package track

import (
	"testing"
	"time"
)

func TestClean(t *testing.T) {
	cases := []struct{ title, channel, artist, name string }{
		{"The Weeknd - Starboy ft. Daft Punk (Official Video) ft. Daft Punk", "The Weeknd", "The Weeknd", "Starboy"},
		{"Blinding Lights", "The Weeknd - Topic", "The Weeknd", "Blinding Lights"},
		{"Shape of You [Official Music Video]", "Ed Sheeran", "Ed Sheeran", "Shape of You"},
		{"Linkin Park – Numb (Official Music Video) [4K UPGRADE]", "Linkin Park", "Linkin Park", "Numb"},
		{"Believer", "ImagineDragonsVEVO", "ImagineDragons", "Believer"},
		{"Levitating (Lyrics)", "7clouds", "7clouds", "Levitating"},
	}
	for _, c := range cases {
		a, n := Clean(c.title, c.channel)
		if a != c.artist || n != c.name {
			t.Errorf("Clean(%q, %q) = (%q, %q), want (%q, %q)", c.title, c.channel, a, n, c.artist, c.name)
		}
	}
}

func TestFormatDur(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0: "0:00", 59 * time.Second: "0:59", 3*time.Minute + 50*time.Second: "3:50",
		time.Hour + 2*time.Minute + 3*time.Second: "1:02:03", -time.Second: "0:00",
	} {
		if got := FormatDur(d); got != want {
			t.Errorf("FormatDur(%v) = %q, want %q", d, got, want)
		}
	}
}
