package lyrics

import (
	"testing"
	"time"
)

func TestParseSynced(t *testing.T) {
	l := parse(result{SyncedLyrics: "[00:12.50] Hello\n[00:05.00] Intro\n[01:02.00][01:30.00] Chorus\n[00:20.00]"})
	if !l.Synced || len(l.Lines) != 5 {
		t.Fatalf("got synced=%v lines=%d", l.Synced, len(l.Lines))
	}
	if l.Lines[0].Text != "Intro" || l.Lines[0].At != 5*time.Second {
		t.Errorf("first line = %+v", l.Lines[0])
	}
	if l.Lines[4].Text != "Chorus" || l.Lines[4].At != 90*time.Second {
		t.Errorf("last line = %+v", l.Lines[4])
	}
	for pos, want := range map[time.Duration]int{0: -1, 5 * time.Second: 0, 13 * time.Second: 1, 2 * time.Minute: 4} {
		if got := l.Index(pos); got != want {
			t.Errorf("Index(%v) = %d, want %d", pos, got, want)
		}
	}
}

func TestParsePlain(t *testing.T) {
	l := parse(result{PlainLyrics: "a\nb"})
	if l.Synced || len(l.Lines) != 2 || l.Index(time.Minute) != -1 {
		t.Errorf("plain parse wrong: %+v", l)
	}
}
