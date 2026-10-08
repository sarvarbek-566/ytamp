package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sarvarbek-566/ytamp/internal/config"
	"github.com/sarvarbek-566/ytamp/internal/player"
	"github.com/sarvarbek-566/ytamp/internal/store"
	"github.com/sarvarbek-566/ytamp/internal/track"
	"github.com/sarvarbek-566/ytamp/internal/youtube"
	"github.com/sarvarbek-566/ytamp/internal/ytdl"
	"golang.org/x/oauth2"
)

// search types q into a fresh search box and submits it.
func (h *harness) search(q string) {
	h.t.Helper()
	for i := 0; i < 5 && (h.m.help || h.m.picker || h.m.filterOn || h.m.searchOn || h.m.scr == screenLogin); i++ {
		h.keys("esc") // leave whatever modal state we are in
	}
	h.keys("/")
	if !h.m.searchOn {
		h.t.Fatalf("'/' did not open search (view=%d focus=%d)", h.m.view, h.m.focus)
	}
	h.typeText(q)
	h.keys("enter")
}

// The bug the user reported: search → play → search again must work, from
// every view and focus.
func TestSearchAgainWhilePlaying(t *testing.T) {
	h := newHarness(t, hopt{})
	h.search("starboy")
	if got := len(h.m.list); got != 12 {
		t.Fatalf("results = %d", got)
	}
	h.keys("enter")
	h.loaded()
	first := h.cur()
	if !strings.Contains(h.pl.last("load"), first.ID) {
		t.Fatalf("first track not loaded: %v", h.pl.calls)
	}
	for _, setup := range [][]string{{}, {"2"}, {"3"}, {"tab"}, {"2", "tab"}, {"3", "h"}} {
		h.keys(setup...)
		h.keys("/")
		if !h.m.searchOn || h.m.view != viewTracks || !strings.Contains(h.screen(), "⌕") {
			t.Fatalf("after %v: search box not visible (searchOn=%v view=%d)", setup, h.m.searchOn, h.m.view)
		}
		h.keys("esc")
	}
	h.search("blinding lights")
	if h.m.listTitle != "Qidiruv: blinding lights" || h.m.list[0].Title != "blinding lights song 0" {
		t.Fatalf("second search not shown: %q / %q", h.m.listTitle, h.m.list[0].Title)
	}
	if h.cur().ID != first.ID {
		t.Fatal("searching must not interrupt the playing track")
	}
	h.keys("j", "j", "enter")
	if h.cur().Title != "blinding lights song 2" || !strings.Contains(h.pl.last("load"), h.cur().ID) {
		t.Fatalf("picked wrong track: %q", h.cur().Title)
	}
}

func TestSearchInputHandlesTypingAndEditing(t *testing.T) {
	h := newHarness(t, hopt{})
	h.keys("/")
	h.typeText("qnpjk /12 ") // global hotkeys must be typed as text here
	if v := h.m.input.Value(); v != "qnpjk /12 " {
		t.Fatalf("input = %q", v)
	}
	if h.quit {
		t.Fatal("'q' inside the search box quit the app")
	}
	h.keys("backspace", "backspace")
	if v := h.m.input.Value(); v != "qnpjk /1" {
		t.Fatalf("after backspace input = %q", v)
	}
	h.keys("esc")
	if h.m.searchOn {
		t.Fatal("esc did not close search")
	}
	h.keys("/", "enter") // empty query: nothing happens
	if h.m.searchOn || h.m.loading != "" {
		t.Fatal("empty search should just close")
	}
	h.keys("/")
	h.typeText("   ")
	h.keys("enter")
	if h.m.loading != "" {
		t.Fatal("whitespace search should be ignored")
	}
}

func TestSearchErrorsAndEmpty(t *testing.T) {
	h := newHarness(t, hopt{})
	h.search("nothing")
	if len(h.m.list) != 0 || !strings.Contains(h.m.toast.text, "topilmadi") {
		t.Fatalf("empty result handling: list=%d toast=%q", len(h.m.list), h.m.toast.text)
	}
	h.keys("enter", "a", "e", "w", "o", "c", "b", "F") // all no-ops on an empty list
	h.searchErr = errors.New("dial tcp: lookup www.youtube.com: no such host")
	h.search("x")
	if !strings.Contains(h.m.toast.text, "Internet") || h.m.loading != "" {
		t.Fatalf("network error: toast=%q loading=%q", h.m.toast.text, h.m.loading)
	}
	h.searchErr = errors.New(`yt-dlp: query "x" page 1: Unable to download API page: HTTPSConnection(host='www.youtube.com', port=443): Failed to resolve`)
	h.search("x")
	if !strings.Contains(h.m.toast.text, "Internet") {
		t.Fatalf("network error: toast=%q loading=%q", h.m.toast.text, h.m.loading)
	}
}

func TestStaleSearchIsIgnored(t *testing.T) {
	h := newHarness(t, hopt{})
	slow := h.m.doSearch("old", false)
	fast := h.m.doSearch("new", false)
	h.run(fast)
	h.run(slow) // arrives last but belongs to a superseded query
	if h.m.listTitle != "Qidiruv: new" || h.m.list[0].Title != "new song 0" {
		t.Fatalf("stale result won: %q", h.m.listTitle)
	}
}

func TestURLAndPaste(t *testing.T) {
	h := newHarness(t, hopt{})
	h.search("https://youtu.be/34Na4j8AVgA?si=x")
	if h.cur().ID != "34Na4j8AVgA" {
		t.Fatalf("URL not played: %+v", h.cur())
	}
	h.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("  https://www.youtube.com/watch?v=XXYlFuWEuKI "), Paste: true})
	if !h.m.searchOn || h.m.input.Value() != "https://www.youtube.com/watch?v=XXYlFuWEuKI" {
		t.Fatalf("paste should open search prefilled, got %q", h.m.input.Value())
	}
	h.keys("enter")
	if h.cur().ID != "XXYlFuWEuKI" {
		t.Fatalf("pasted URL not played: %+v", h.cur())
	}
}

func TestCLIQueryPlaysTopHit(t *testing.T) {
	h := newHarness(t, hopt{opts: func(o *Options) { o.PlayQuery = "the weeknd" }})
	if h.cur().Title != "the weeknd song 0" || h.pl.count("load ") != 1 {
		t.Fatalf("cli query: cur=%q calls=%v", h.cur().Title, h.pl.calls)
	}
}

func TestFilter(t *testing.T) {
	h := newHarness(t, hopt{})
	h.search("abc")
	h.keys("f")
	h.typeText("song 1")
	if len(h.m.shown) != 3 { // song 1, 10, 11
		t.Fatalf("filtered = %d", len(h.m.shown))
	}
	h.keys("down", "enter") // enter plays the highlighted match
	if h.cur().Title != "abc song 10" {
		t.Fatalf("enter in filter played %q", h.cur().Title)
	}
	if len(h.m.queue) < 12 || h.m.queue[11].Title != "abc song 11" {
		t.Fatalf("queue should start with the whole list, got %v", titles(h.m.queue))
	}
	h.keys("f")
	h.typeText("zzz")
	if len(h.m.shown) != 0 {
		t.Fatal("expected no matches")
	}
	h.keys("enter") // nothing to play: must not crash or change track
	if h.cur().Title != "abc song 10" {
		t.Fatal("enter on empty filter changed track")
	}
	h.keys("esc", "f", "esc")
	if h.m.filter.Value() != "" || len(h.m.shown) != 12 {
		t.Fatal("esc should clear the filter")
	}
	h.keys("3", "f") // filter from lyrics view switches back to the list
	if h.m.view != viewTracks {
		t.Fatal("filter from lyrics view")
	}
}

func TestPlaybackControls(t *testing.T) {
	h := newHarness(t, hopt{cfg: func(c *config.Config) { c.Autoplay = false }})
	h.keys("space", "n", "p", "right", "left", "s", "s", "r", "r", "r", "+", "-", "m") // nothing loaded: no crash
	h.search("abc")
	h.keys("enter")
	h.loaded()
	h.keys("space")
	if h.pl.last("toggle") == "" {
		t.Fatal("space did not toggle pause")
	}
	h.ev(player.PropEvent{Name: "pause", Value: true})
	h.keys("space")
	if h.pl.count("toggle") != 2 {
		t.Fatal("second space")
	}
	h.ev(player.PropEvent{Name: "time-pos", Value: 30.0})
	h.keys("right", "L", "left", "H")
	if h.pl.count("seekto ") != 4 || h.pl.last("seekto") != "seekto 30" {
		t.Fatalf("seeks: %v", h.pl.calls)
	}
	h.keys("n") // seek while the next song is still loading is kept…
	h.keys("L", "L")
	if h.pl.last("seekto") != "seekto 30" {
		t.Fatal("seek was sent before the file loaded")
	}
	h.loaded() // …and applied once it loads
	if h.pl.last("seekto") != "seekto 60" {
		t.Fatalf("pending seek not applied: %q", h.pl.last("seekto"))
	}
	if h.cur().Title != "abc song 1" {
		t.Fatalf("n → %q", h.cur().Title)
	}
	h.ev(player.PropEvent{Name: "time-pos", Value: 10.0})
	h.keys("p") // >4s in: restart the song
	if h.cur().Title != "abc song 1" || h.pl.last("seekto") != "seekto 0" {
		t.Fatal("p after 4s should restart")
	}
	h.ev(player.PropEvent{Name: "time-pos", Value: 1.0})
	h.keys("p")
	if h.cur().Title != "abc song 0" {
		t.Fatalf("p → %q", h.cur().Title)
	}
	h.keys("p") // first track: restart, no wrap
	if h.cur().Title != "abc song 0" {
		t.Fatal("p on first track")
	}
	for i := 0; i < 30; i++ {
		h.keys("+")
	}
	if h.m.volume != 130 {
		t.Fatalf("volume max = %v", h.m.volume)
	}
	for i := 0; i < 40; i++ {
		h.keys("-")
	}
	if h.m.volume != 0 {
		t.Fatalf("volume min = %v", h.m.volume)
	}
}

func TestEndOfQueueWithoutAutoplay(t *testing.T) {
	h := newHarness(t, hopt{cfg: func(c *config.Config) { c.Autoplay = false }})
	h.search("abc")
	h.keys("G", "enter") // last track
	h.loaded()
	h.eof()
	h.ev(player.PropEvent{Name: "idle-active", Value: true})
	if !h.m.ended || h.pl.last("stop") == "" {
		t.Fatalf("should stop at end: ended=%v", h.m.ended)
	}
	if !strings.Contains(h.screen(), "Tayyor") {
		t.Fatal("status should read Tayyor after the queue ends")
	}
	h.keys("g", "a") // queue the first song after the end
	h.keys("space")
	if h.cur().Title != "abc song 0" {
		t.Fatalf("space after end should play the newly queued song, got %q", h.cur().Title)
	}
	h.loaded()
	h.eof()
	h.ev(player.PropEvent{Name: "idle-active", Value: true})
	h.keys("space") // nothing new: replay the last one
	if h.cur().Title != "abc song 0" || h.m.ended {
		t.Fatal("space at end with nothing new should replay")
	}
}

func TestAutoplayRadioContinues(t *testing.T) {
	h := newHarness(t, hopt{})
	h.search("abc")
	h.keys("G", "enter")
	h.loaded()
	// prefetch already appended the radio because we are near the end
	if len(h.m.queue) != 12+25 {
		t.Fatalf("radio prefetch: queue=%d", len(h.m.queue))
	}
	h.eof()
	if !strings.HasPrefix(h.cur().Title, "radio-") {
		t.Fatalf("after eof expected radio track, got %q", h.cur().Title)
	}
}

func TestAutoplayRadioArrivesAfterEnd(t *testing.T) {
	h := newHarness(t, hopt{})
	h.radioErr = errors.New("offline")
	h.search("abc")
	h.keys("G", "enter") // prefetch fails
	h.loaded()
	h.radioErr = nil
	h.eof() // now radio succeeds and must start playing
	if !strings.HasPrefix(h.cur().Title, "radio-") || h.m.awaitRadio {
		t.Fatalf("radio after end: cur=%q await=%v", h.cur().Title, h.m.awaitRadio)
	}
}

func TestAutoplayRadioFailsStopsCleanly(t *testing.T) {
	h := newHarness(t, hopt{})
	h.radioErr = errors.New("offline")
	h.search("abc")
	h.keys("G", "enter")
	h.loaded()
	h.eof()
	if !h.m.ended || h.m.starting || h.m.awaitRadio {
		t.Fatalf("should stop cleanly: ended=%v starting=%v await=%v", h.m.ended, h.m.starting, h.m.awaitRadio)
	}
	if strings.Contains(h.screen(), "Yuklanmoqda") {
		t.Fatal("stuck on a loading spinner")
	}
}

func TestRadioWithOnlyDuplicatesStops(t *testing.T) {
	h := newHarness(t, hopt{})
	h.radioN = 0
	h.search("abc")
	h.keys("G", "enter")
	h.loaded()
	h.eof()
	if !h.m.ended || h.m.starting {
		t.Fatal("empty radio should stop, not spin forever")
	}
}

func TestRepeatModes(t *testing.T) {
	h := newHarness(t, hopt{})
	h.search("abc")
	h.keys("r", "r") // repeat one
	h.keys("enter")
	h.loaded()
	h.eof()
	if h.cur().Title != "abc song 0" {
		t.Fatalf("repeat one: %q", h.cur().Title)
	}
	h.keys("n") // manual next still advances
	if h.cur().Title != "abc song 1" {
		t.Fatalf("n in repeat-one: %q", h.cur().Title)
	}
	h.keys("r", "r") // one → off → all
	if h.m.repeat != repeatAll {
		t.Fatalf("repeat = %d", h.m.repeat)
	}
	h.keys("G")
	h.keys("enter")
	h.loaded()
	h.eof()
	if h.cur().Title != "abc song 0" {
		t.Fatalf("repeat all wrap: %q", h.cur().Title)
	}
}

func TestShuffleKeepsQueueIntact(t *testing.T) {
	h := newHarness(t, hopt{})
	h.search("abc")
	h.keys("j", "j", "enter") // song 2
	h.loaded()
	h.keys("s")
	if h.cur().Title != "abc song 2" || len(h.m.queue) != 12 {
		t.Fatal("shuffle changed current track or size")
	}
	h.search("xyz")
	h.keys("a", "a") // add two while shuffled
	h.keys("2")
	h.keys("x") // remove whatever is selected (current is protected)
	before := map[string]bool{}
	for _, tr := range h.m.queue {
		before[tr.ID] = true
	}
	h.keys("s") // unshuffle
	if h.cur().Title != "abc song 2" {
		t.Fatalf("unshuffle moved off current: %q", h.cur().Title)
	}
	after := map[string]bool{}
	for _, tr := range h.m.queue {
		after[tr.ID] = true
	}
	if len(after) != len(before) {
		t.Fatalf("unshuffle lost or added tracks: %d → %d", len(before), len(after))
	}
	for id := range before {
		if !after[id] {
			t.Fatal("track lost on unshuffle")
		}
	}
	// original order restored for the abc tracks
	last := -1
	for _, tr := range h.m.queue {
		if strings.HasPrefix(tr.Title, "abc song ") {
			n := tr.Title[len("abc song "):]
			var k int
			for _, c := range n {
				k = k*10 + int(c-'0')
			}
			if k < last {
				t.Fatalf("order not restored: %v", titles(h.m.queue))
			}
			last = k
		}
	}
	// shuffle on, then play a list: first played is the chosen one
	h.keys("s", "1")
	h.search("pqr")
	h.keys("j", "j", "j", "enter")
	if h.cur().Title != "pqr song 3" || h.m.qi != 0 {
		t.Fatalf("shuffled play-from: %q qi=%d", h.cur().Title, h.m.qi)
	}
}

func titles(ts []track.Track) []string {
	var out []string
	for _, t := range ts {
		out = append(out, t.Title)
	}
	return out
}

func TestEnqueueGoesBeforeRadio(t *testing.T) {
	h := newHarness(t, hopt{})
	h.search("abc")
	h.keys("enter") // full list queued; nothing near the end yet
	h.keys("G", "enter")
	h.loaded() // last song: radio is appended
	h.keys("g", "a", "a")
	if h.m.queue[h.m.qi+1].Title != "abc song 0" || h.m.queue[h.m.qi+2].Title != "abc song 1" {
		t.Fatalf("user picks should come before radio: %v", titles(h.m.queue[h.m.qi:h.m.qi+4]))
	}
	h.eof()
	if h.cur().Title != "abc song 0" {
		t.Fatalf("next should be the user's pick, got %q", h.cur().Title)
	}
}

func TestQueueEditing(t *testing.T) {
	h := newHarness(t, hopt{cfg: func(c *config.Config) { c.Autoplay = false }})
	h.search("abc")
	h.keys("a") // empty queue: add song 0, nothing plays yet
	if len(h.m.queue) != 1 || h.pl.count("load") != 0 {
		t.Fatal("a on empty queue")
	}
	h.keys("space")
	if h.cur().Title != "abc song 0" {
		t.Fatal("space should start the queued song")
	}
	h.loaded()
	h.keys("a", "a") // songs 1, 2 appended (cursor advances)
	h.keys("e")      // song 3 as next
	h.keys("2")      // queue view
	order := titles(h.m.queue)
	want := []string{"abc song 0", "abc song 3", "abc song 1", "abc song 2"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("queue = %v", order)
	}
	h.keys("x") // cursor on current → refused
	if len(h.m.queue) != 4 {
		t.Fatal("removed the playing song")
	}
	h.keys("j", "J") // move "song 3" down
	if h.m.queue[2].Title != "abc song 3" || h.m.qCur != 2 {
		t.Fatalf("J: %v", titles(h.m.queue))
	}
	h.keys("g", "J") // move the current track down: qi must follow it
	if h.cur().Title != "abc song 0" || h.m.qi != 1 {
		t.Fatalf("moving current: qi=%d cur=%q", h.m.qi, h.cur().Title)
	}
	h.keys("g", "x") // remove the song before current
	if h.m.qi != 0 || h.cur().Title != "abc song 0" {
		t.Fatalf("remove before current: qi=%d", h.m.qi)
	}
	h.keys("G", "x", "x", "x", "x", "x") // remove until only current remains
	if len(h.m.queue) != 1 || h.cur().Title != "abc song 0" {
		t.Fatalf("queue = %v", titles(h.m.queue))
	}
	h.keys("K", "J", "enter")
	if h.cur().Title != "abc song 0" {
		t.Fatal("enter in queue")
	}
}

func TestPlaybackErrorsRetryThenSkip(t *testing.T) {
	h := newHarness(t, hopt{cfg: func(c *config.Config) { c.Autoplay = false }})
	h.search("abc")
	h.keys("enter")
	h.ev(player.PropEvent{Name: "time-pos", Value: 12.0})
	h.ev(player.EndEvent{Reason: "error", Err: "no audio or video data played"})
	if h.pl.last("load") != "load https://stream.example/"+h.cur().ID+" @12" {
		t.Fatalf("retry should reload a fresh stream at the same spot: %q", h.pl.last("load"))
	}
	h.ev(player.EndEvent{Reason: "error"})
	if h.cur().Title != "abc song 1" {
		t.Fatalf("second failure should skip: %q", h.cur().Title)
	}
	h.streamErr = errors.New("403")
	for i := 0; i < 8; i++ { // every track fails: must stop, not loop forever
		h.ev(player.EndEvent{Reason: "error"})
	}
	if !h.m.ended || h.m.errStreak < 4 {
		t.Fatalf("should give up: ended=%v streak=%d cur=%q", h.m.ended, h.m.errStreak, h.cur().Title)
	}
	h.streamErr = nil
	h.keys("n") // user can carry on afterwards
	h.loaded()
	if h.m.errStreak != 0 || h.m.ended {
		t.Fatal("successful load should reset the error streak")
	}
}

func TestPlayerDeathIsSurvivable(t *testing.T) {
	h := newHarness(t, hopt{})
	h.search("abc")
	h.keys("enter")
	h.ev(player.ExitEvent{Err: errors.New("killed")})
	if !h.m.plDead {
		t.Fatal("player death not recorded")
	}
	h.keys("space", "n", "p", "right", "+", "m", "enter", "s", "r", "z", "z") // must not panic
	h.m.Shutdown()
}

func TestSessionResume(t *testing.T) {
	dir := t.TempDir()
	h := newHarness(t, hopt{stateDir: dir})
	h.search("abc")
	h.keys("j", "j", "enter")
	h.loaded()
	h.ev(player.PropEvent{Name: "time-pos", Value: 95.0})
	h.keys("+")
	h.m.Shutdown()

	h2 := newHarness(t, hopt{stateDir: dir})
	if got := h2.pl.last("loadpaused"); !strings.Contains(got, h.cur().ID) || !strings.HasSuffix(got, "@95") {
		t.Fatalf("resume: %q", got)
	}
	if h2.cur().Title != "abc song 2" || len(h2.m.queue) != len(h.m.queue) {
		t.Fatal("resume restored wrong queue/track")
	}
	// mpv's startup idle=true must not make space restart from 0
	h2.ev(player.PropEvent{Name: "idle-active", Value: true})
	h2.ev(player.PropEvent{Name: "pause", Value: true})
	h2.keys("space")
	if h2.pl.count("load ") != 0 || h2.pl.last("toggle") == "" {
		t.Fatalf("space during resume should unpause, calls=%v", h2.pl.calls)
	}
	h2.ev(player.LoadedEvent{})
	h2.ev(player.PropEvent{Name: "idle-active", Value: false})
	if h2.m.idle || h2.m.starting {
		t.Fatal("state after resume load")
	}
	// an empty session must not clobber the saved one
	h3 := newHarness(t, hopt{stateDir: dir, cfg: func(c *config.Config) { c.ResumeSession = false }})
	h3.m.Shutdown()
	if s, _ := store.LoadSession(); len(s.Queue) == 0 {
		t.Fatal("empty run overwrote the saved session")
	}
}

func TestResumeRetryKeepsPausedPosition(t *testing.T) {
	dir := t.TempDir()
	h := newHarness(t, hopt{stateDir: dir})
	h.search("abc")
	h.keys("enter")
	h.loaded()
	h.ev(player.PropEvent{Name: "time-pos", Value: 40.0})
	h.m.Shutdown()
	h2 := newHarness(t, hopt{stateDir: dir})
	h2.ev(player.EndEvent{Reason: "error"})
	if got := h2.pl.last("loadpaused"); got != "loadpaused https://stream.example/"+h2.cur().ID+" @40" {
		t.Fatalf("retry during resume lost state: %q", got)
	}
}

func TestViewsAndLyrics(t *testing.T) {
	h := newHarness(t, hopt{})
	h.keys("3") // nothing playing
	if !strings.Contains(h.screen(), "ijro etilmayapti") {
		t.Fatal("lyrics view without track")
	}
	h.search("abc")
	h.keys("enter")
	h.loaded()
	h.keys("3")
	h.ev(player.PropEvent{Name: "time-pos", Value: 6.0})
	if !strings.Contains(h.screen(), "› second ‹") {
		t.Fatalf("synced lyric line not highlighted:\n%s", h.screen())
	}
	h.keys("1", "n") // song 1 has no lyrics in the fake
	h.keys("3")
	if !strings.Contains(h.screen(), "matn topilmadi") {
		t.Fatal("missing lyrics message")
	}
	h.keys("2")
	if !strings.Contains(h.screen(), "Navbat") {
		t.Fatal("queue view")
	}
	h.keys("?")
	if !strings.Contains(h.screen(), "yordam") && !strings.Contains(h.screen(), "Ijro") {
		t.Fatal("help")
	}
	h.keys("j", "n", "/") // keys inside help do nothing
	if h.m.searchOn || !h.m.help {
		t.Fatal("help should swallow keys")
	}
	h.keys("esc")
	for i := 0; i < 8; i++ {
		h.keys("t") // every theme renders
	}
	for i := 0; i < 3; i++ {
		h.keys("v") // every visualizer mode renders
	}
}

func TestSidebarNavigation(t *testing.T) {
	h := newHarness(t, hopt{})
	h.keys("h", "j") // onto "Qidiruv": browsing must not open the search box
	if h.m.searchOn {
		t.Fatal("browsing onto Qidiruv opened search")
	}
	h.keys("j") // Radio: browsing must not fetch
	if h.m.loading != "" {
		t.Fatal("browsing onto Radio started a fetch")
	}
	h.keys("enter") // activating Radio with nothing playing: hint only
	h.keys("h", "k", "enter")
	if !h.m.searchOn {
		t.Fatal("enter on empty Qidiruv should open search")
	}
	h.keys("esc")
	for i := 0; i < 10; i++ {
		h.keys("j")
	}
	for i := 0; i < 10; i++ {
		h.keys("k")
	}
	h.keys("G", "g", "tab", "tab", "shift+tab", "l")
}

func TestRadioKey(t *testing.T) {
	h := newHarness(t, hopt{})
	h.search("abc")
	h.keys("enter")
	h.loaded()
	h.keys("2", "j", "w") // radio from the selected queue item, shown on screen
	if h.m.view != viewTracks || h.m.sources[h.m.srcCur].kind != srcRadio {
		t.Fatal("w should switch to the radio list")
	}
	if len(h.m.list) != 25 || !strings.Contains(h.m.list[0].Title, "radio-"+id("abc1")) {
		t.Fatalf("radio list = %d %q", len(h.m.list), h.m.list[0].Title)
	}
	// stale radio for an older seed must be ignored
	old := h.m.loadRadio("aaaaaaaaaaa", false)
	newer := h.m.loadRadio("bbbbbbbbbbb", false)
	h.run(newer)
	h.run(old)
	if !strings.Contains(h.m.list[0].Title, "bbbbbbbbbbb") {
		t.Fatalf("stale radio won: %q", h.m.list[0].Title)
	}
}

func TestHistory(t *testing.T) {
	h := newHarness(t, hopt{})
	h.search("abc")
	h.keys("enter")
	h.keys("n", "n", "p")
	h.keys("h", "g", "enter") // History
	if h.m.list[0].Title != "abc song 1" || len(h.m.list) != 3 {
		t.Fatalf("history = %v", titles(h.m.list))
	}
	h.keys("j", "enter") // play from history; history list updates live
	if h.cur().Title != "abc song 2" || h.m.list[0].Title != "abc song 2" {
		t.Fatalf("history live update: cur=%q top=%q", h.cur().Title, h.m.list[0].Title)
	}
	if t2, _, _ := h.m.selected(); t2.Title != "abc song 2" {
		t.Fatalf("cursor should follow the song, on %q", t2.Title)
	}
}

func TestSleepTimer(t *testing.T) {
	h := newHarness(t, hopt{})
	h.search("abc")
	h.keys("enter")
	h.loaded()
	h.keys("z")
	if h.m.sleepAt.IsZero() {
		t.Fatal("z should arm the timer")
	}
	h.keys("z", "z", "z", "z")
	if !h.m.sleepAt.IsZero() {
		t.Fatal("cycling past 90 should disarm")
	}
	h.keys("z")
	vol := h.m.volume
	h.m.sleepAt = time.Now().Add(10 * time.Second) // into the fade window
	h.send(tickMsg(time.Now().Add(2 * time.Second)))
	if !strings.HasPrefix(h.pl.last("vol"), "vol ") {
		t.Fatal("fade did not touch volume")
	}
	h.m.sleepAt = time.Now().Add(-time.Second)
	h.send(tickMsg(time.Now().Add(4 * time.Second)))
	if h.pl.last("pause") != "pause true" || h.m.volume != vol {
		t.Fatalf("timer end: %q vol=%v want %v", h.pl.last("pause"), h.m.volume, vol)
	}
}

func TestMpris(t *testing.T) {
	h := newHarness(t, hopt{})
	for _, op := range []string{"playpause", "play", "pause", "next", "prev", "stop", "seek", "setpos"} {
		h.send(mprisMsg{op: op, arg: 5_000_000}) // nothing loaded: no crash
	}
	h.search("abc")
	h.keys("enter")
	h.loaded()
	h.send(mprisMsg{op: "next"})
	if h.cur().Title != "abc song 1" {
		t.Fatal("mpris next")
	}
	h.loaded()
	h.send(mprisMsg{op: "seek", arg: 10_000_000})
	if h.pl.last("seekto") != "seekto 10" {
		t.Fatal("mpris seek")
	}
	h.send(mprisMsg{op: "setpos", arg: 42_000_000})
	if h.pl.last("seekto") != "seekto 42" {
		t.Fatal("mpris setpos")
	}
	h.send(mprisMsg{op: "playpause"})
	if h.pl.last("toggle") == "" {
		t.Fatal("mpris playpause")
	}
	h.send(mprisMsg{op: "quit"})
	if !h.quit {
		t.Fatal("mpris quit")
	}
}

func TestOpenAndCopy(t *testing.T) {
	h := newHarness(t, hopt{})
	h.keys("o", "c") // nothing selected
	if len(h.opened)+len(h.copied) != 0 {
		t.Fatal("o/c with nothing should do nothing")
	}
	h.search("abc")
	h.keys("j", "o", "c")
	want := "https://www.youtube.com/watch?v=" + id("abc1")
	if len(h.opened) != 1 || h.opened[0] != want || h.copied[0] != want {
		t.Fatalf("opened=%v copied=%v", h.opened, h.copied)
	}
}

func TestQuitKeys(t *testing.T) {
	for _, k := range []string{"q", "ctrl+c"} {
		h := newHarness(t, hopt{})
		h.keys(k)
		if !h.quit {
			t.Fatalf("%s did not quit", k)
		}
	}
	h := newHarness(t, hopt{})
	h.keys("/", "ctrl+c") // ctrl+c works even while typing
	if !h.quit {
		t.Fatal("ctrl+c in search")
	}
}

// ---------- logged-in flows ----------

func loggedIn(t *testing.T, yt *fakeYT) *harness {
	h := newHarness(t, hopt{})
	h.send(clientMsg{yt: yt, user: "sar1k"})
	return h
}

func TestLibraryFlows(t *testing.T) {
	yt := &fakeYT{
		liked: fakeTracks("liked", 5),
		pls:   []youtube.Playlist{{ID: "PL1", Title: "Chill", Count: 3}, {ID: "PL2", Title: "Gym", Count: 2}},
		plTracks: map[string][]track.Track{
			"PL1": fakeTracks("chill", 3), "PL2": fakeTracks("gym", 2),
		},
	}
	h := loggedIn(t, yt)
	if h.m.sources[0].kind != srcLiked || len(h.m.list) != 5 {
		t.Fatalf("liked not shown: sources=%v list=%d", h.m.sources, len(h.m.list))
	}
	if !strings.Contains(h.screen(), "sar1k") || strings.Contains(h.screen(), "oflayn") {
		t.Fatal("header user")
	}
	h.keys("h", "G") // last playlist (Gym) — loads lazily
	if len(h.m.list) != 2 || h.m.list[0].Title != "gym song 0" {
		t.Fatalf("playlist load: %v", titles(h.m.list))
	}
	h.keys("l", "enter")
	h.loaded()
	h.keys("F") // like the playing song
	if !yt.rated[h.cur().ID] || !h.m.isLiked(h.cur().ID) || len(h.m.lib.Liked) != 6 {
		t.Fatal("like")
	}
	h.keys("F") // unlike
	if yt.rated[h.cur().ID] || h.m.isLiked(h.cur().ID) || len(h.m.lib.Liked) != 5 {
		t.Fatal("unlike")
	}
	h.keys("b", "j", "j", "k", "enter") // add to playlist via picker
	if len(yt.added) != 1 || yt.added[0] != "PL2:"+h.m.list[h.m.shown[h.m.listCur]].ID {
		t.Fatalf("added = %v", yt.added)
	}
	h.keys("b", "esc")
	if h.m.picker {
		t.Fatal("esc should close picker")
	}
	// R spam: only one refresh may be in flight
	var cmds int
	for i := 0; i < 3; i++ {
		if h.m.onKey(keyMsg("R")) != nil {
			cmds++
		}
	}
	if cmds != 1 {
		t.Fatalf("R spam started %d fetches", cmds)
	}
}

func TestLibraryErrors(t *testing.T) {
	yt := &fakeYT{fail: errors.New("googleapi: Error 403: quotaExceeded")}
	h := newHarness(t, hopt{})
	h.m.connecting = true
	h.send(clientMsg{err: yt.fail})
	if h.m.connecting || !strings.Contains(h.m.toast.text, "kvota") {
		t.Fatalf("connect error: connecting=%v toast=%q", h.m.connecting, h.m.toast.text)
	}
	h2 := loggedIn(t, yt) // Me ok in the message, but library fetch fails
	if h2.m.loading != "" || h2.m.libBusy {
		t.Fatal("failed library fetch left a spinner")
	}
	h2.keys("R")
	if h2.m.libBusy {
		t.Fatal("libBusy stuck after error")
	}
	h3 := newHarness(t, hopt{})
	h3.keys("F", "b", "R") // guest: friendly messages, no crash
	if h3.m.picker {
		t.Fatal("picker opened without account")
	}
}

func loginHarness(t *testing.T, cfg func(*config.Config)) *harness {
	return newHarness(t, hopt{cfg: cfg, opts: func(o *Options) { o.Guest = false; o.ForceLogin = true }})
}

func TestLoginBrowserHappyPath(t *testing.T) {
	h := loginHarness(t, nil)
	if h.m.scr != screenLogin || h.m.stage != stageChoose {
		t.Fatal("--login should open the chooser")
	}
	scr := h.screen()
	for _, want := range []string{"Firefox orqali", "(tavsiya)", "Chromium orqali", "Google OAuth", "Mehmon rejimi"} {
		if !strings.Contains(scr, want) {
			t.Fatalf("chooser missing %q:\n%s", want, scr)
		}
	}
	h.keys("enter") // Firefox
	if h.m.scr != screenMain || h.m.yt == nil || h.m.user != "sar1k" {
		t.Fatalf("browser login: scr=%d yt=%v user=%q err=%q", h.m.scr, h.m.yt != nil, h.m.user, h.m.loginErr)
	}
	if h.saved["cookies_from_browser"] != "firefox" || h.m.cfg.CookiesFromBrowser != "firefox" {
		t.Fatalf("browser not remembered: %v", h.saved)
	}
	if len(h.m.lib.Liked) != 7 || h.m.sources[0].kind != srcLiked || len(h.m.list) != 7 {
		t.Fatalf("library after login: liked=%d list=%d", len(h.m.lib.Liked), len(h.m.list))
	}
	if !strings.Contains(h.screen(), "● sar1k") {
		t.Fatal("header should show the account")
	}
	h.keys("enter")
	h.loaded()
	id := h.cur().ID // a song from Liked: F un-likes, F again likes
	h.keys("F")
	if liked, called := h.browserYT.rated[id]; !called || liked || h.m.isLiked(id) {
		t.Fatal("unlike in browser mode")
	}
	h.keys("F")
	if !h.browserYT.rated[id] || !h.m.isLiked(id) {
		t.Fatal("like in browser mode")
	}
}

func TestLoginBrowserFailureAndRetry(t *testing.T) {
	h := loginHarness(t, nil)
	h.browserErr = ytdl.ErrNotSignedIn
	h.keys("enter")
	if h.m.stage != stageBrowser || !strings.Contains(h.screen(), "kirilmagan") || !strings.Contains(h.screen(), "youtube.com") {
		t.Fatalf("failure should explain what to do:\n%s", h.screen())
	}
	if _, ok := h.saved["cookies_from_browser"]; ok {
		t.Fatal("a failed browser must not be saved")
	}
	h.browserErr = nil
	h.keys("r")
	if h.m.scr != screenMain || h.m.yt == nil {
		t.Fatal("retry after signing in should succeed")
	}
	h2 := loginHarness(t, nil)
	h2.browserErr = errors.New("boom")
	h2.keys("j", "enter", "esc") // Chromium fails → back to chooser
	if h2.m.stage != stageChoose || h2.m.loginErr != "" {
		t.Fatal("esc should return to the chooser")
	}
	h2.keys("esc") // leave as guest
	if h2.m.scr != screenMain || h2.m.yt != nil {
		t.Fatal("esc from chooser → guest")
	}
}

func TestLoginStaleBrowserResultIgnored(t *testing.T) {
	h := loginHarness(t, nil)
	h.m.stage, h.m.tryingBrowser = stageBrowser, ytdl.Browser{Name: "firefox", Label: "Firefox"}
	h.keys("esc", "esc") // user gave up and went to the player
	h.send(browserMsg{browser: "firefox", yt: h.browserYT, user: "late"})
	if h.m.yt != nil || len(h.saved) != 0 {
		t.Fatal("a late browser result must not sign the user in")
	}
}

func TestLoginOAuthForm(t *testing.T) {
	h := loginHarness(t, nil)
	h.keys("j", "j", "enter") // Google OAuth
	if h.m.stage != stageOAuthForm {
		t.Fatalf("stage = %d", h.m.stage)
	}
	h.keys("enter", "enter") // empty
	if !strings.Contains(h.m.loginErr, "ikkalasi") {
		t.Fatalf("empty form: %q", h.m.loginErr)
	}
	h.keys("tab")
	h.typeText("not-a-client-id")
	h.keys("tab")
	h.typeText("q-secret") // 'q' must type, not quit
	if h.quit {
		t.Fatal("q quit from a text field")
	}
	h.keys("enter")
	if !strings.Contains(h.m.loginErr, "googleusercontent") {
		t.Fatalf("bad id: %q", h.m.loginErr)
	}
	h.keys("tab")
	for range "not-a-client-id" {
		h.keys("backspace")
	}
	h.typeText("123-abc.apps.googleusercontent.com")
	if strings.Contains(h.screen(), "q-secret") {
		t.Fatal("secret must be masked on screen")
	}
	h.keys("ctrl+o")
	if len(h.opened) != 1 || !strings.Contains(h.opened[0], "console.cloud.google.com") {
		t.Fatalf("ctrl+o: %v", h.opened)
	}
	h.keys("tab", "enter")
	if h.m.stage != stageDevice || h.m.oc == nil {
		t.Fatal("should move on to the device code")
	}
	if len(h.saved) != 0 {
		t.Fatalf("credentials saved before Google accepted them: %v", h.saved)
	}
	// Google rejects the client: nothing saved, esc goes back to the form
	h.send(deviceMsg{err: errors.New(`oauth2: "invalid_client"`)})
	if !strings.Contains(h.screen(), "tanimadi") || len(h.saved) != 0 {
		t.Fatalf("device error:\n%s", h.screen())
	}
	h.keys("esc")
	if h.m.stage != stageOAuthForm || h.m.idIn.Value() != "123-abc.apps.googleusercontent.com" {
		t.Fatal("esc after a rejected client should return to the filled form")
	}
	h.keys("enter")
	h.send(deviceMsg{da: &oauth2.DeviceAuthResponse{UserCode: "ABCD-EFGH", VerificationURI: "https://google.com/device", Expiry: time.Now().Add(time.Minute)}})
	if !strings.Contains(h.screen(), "A B C D - E F G H") {
		t.Fatal("device code not shown")
	}
	if h.saved["client_id"] != "123-abc.apps.googleusercontent.com" || h.saved["client_secret"] != "q-secret" {
		t.Fatalf("accepted credentials not saved: %v", h.saved)
	}
	h.keys("o", "c")
	if h.opened[len(h.opened)-1] != "https://google.com/device" || h.copied[0] != "ABCD-EFGH" {
		t.Fatal("o/c on device screen")
	}
	h.keys("esc", "esc") // back out (device → form → chooser): polling is cancelled
	if h.m.stage != stageChoose || h.m.da != nil {
		t.Fatal("esc from device")
	}
	h.send(tokenMsg{tok: &oauth2.Token{AccessToken: "x"}}) // late token after backing out
	if h.m.scr != screenLogin || h.m.stage != stageChoose {
		t.Fatal("late token acted on")
	}
	h.send(deviceMsg{da: &oauth2.DeviceAuthResponse{UserCode: "LATE"}})
	if h.m.da != nil {
		t.Fatal("late device code acted on")
	}
	// prefilled on a second visit, focus jumps to the secret
	h.keys("j", "j", "enter")
	if h.m.idIn.Value() != "123-abc.apps.googleusercontent.com" || h.m.formFocus != 1 {
		t.Fatal("form should be prefilled")
	}
}

func TestLoginOAuthTokenSignsIn(t *testing.T) {
	h := loginHarness(t, func(c *config.Config) { c.ClientID, c.ClientSecret = "id.apps.googleusercontent.com", "s" })
	h.keys("j", "j", "enter", "enter")
	if h.m.stage != stageDevice {
		t.Fatal("prefilled form should submit")
	}
	h.send(deviceMsg{da: &oauth2.DeviceAuthResponse{UserCode: "X", Expiry: time.Now().Add(time.Minute)}})
	_, cmd := h.m.Update(tokenMsg{tok: &oauth2.Token{AccessToken: "x"}})
	if h.m.scr != screenMain || !h.m.connecting || cmd == nil {
		t.Fatal("token should leave the login screen and connect")
	}
	h.send(clientMsg{yt: &fakeYT{liked: fakeTracks("o", 3)}, user: "oauth-user"})
	if h.m.connecting || h.m.user != "oauth-user" || len(h.m.list) != 3 {
		t.Fatal("oauth sign-in")
	}
}

func TestStartupWithRememberedBrowser(t *testing.T) {
	h := newHarness(t, hopt{cfg: func(c *config.Config) { c.CookiesFromBrowser = "firefox" }, opts: func(o *Options) { o.Guest = false }})
	if h.m.scr != screenMain || h.m.yt == nil || h.m.user != "sar1k" || len(h.m.lib.Liked) != 7 {
		t.Fatalf("auto sign-in: yt=%v user=%q", h.m.yt != nil, h.m.user)
	}
}

func TestStartupBrowserBrokenFallsBackToGuest(t *testing.T) {
	h2 := newHarness(t, hopt{cfg: func(c *config.Config) { c.CookiesFromBrowser = "firefox" }, opts: func(o *Options) { o.Guest = true }})
	h2.browserErr = ytdl.ErrNotSignedIn
	h2.m.connecting = true
	h2.run(h2.m.connectBrowser("firefox"))
	if h2.m.connecting || h2.m.yt != nil || !strings.Contains(h2.m.toast.text, "A") {
		t.Fatalf("broken session: connecting=%v toast=%q", h2.m.connecting, h2.m.toast.text)
	}
	if !strings.Contains(h2.screen(), "mehmon") {
		t.Fatal("header should show guest")
	}
}

func TestAccountKeyAndLogout(t *testing.T) {
	h := newHarness(t, hopt{})
	h.search("abc")
	h.keys("enter")
	h.loaded()
	h.keys("A")
	if h.m.scr != screenLogin || !strings.Contains(h.screen(), "Orqaga") {
		t.Fatal("A should open the account screen")
	}
	h.keys("esc")
	if h.m.scr != screenMain || h.cur().Title != "abc song 0" {
		t.Fatal("esc returns to the player, playback untouched")
	}
	h.keys("A", "enter") // sign in with Firefox
	if h.m.yt == nil {
		t.Fatal("sign in from inside the app")
	}
	h.keys("A")
	if !strings.Contains(h.screen(), "Akkauntdan chiqish") {
		t.Fatal("logout option missing when signed in")
	}
	h.keys("j", "j", "j", "enter") // firefox, chromium, oauth, logout
	if h.m.yt != nil || h.m.user != "" || len(h.m.lib.Liked) != 0 || h.saved["cookies_from_browser"] != "" {
		t.Fatalf("logout: yt=%v user=%q liked=%d", h.m.yt != nil, h.m.user, len(h.m.lib.Liked))
	}
	if h.m.scr != screenMain || h.cur().Title != "abc song 0" {
		t.Fatal("logout must not stop the music")
	}
	for _, s := range h.m.sources {
		if s.kind == srcLiked || s.kind == srcPlaylist {
			t.Fatal("account sources left after logout")
		}
	}
}

func TestSwitchingAccountsDropsOldLibrary(t *testing.T) {
	h := newHarness(t, hopt{})
	h.send(clientMsg{yt: &fakeYT{liked: fakeTracks("one", 4)}, user: "first"})
	h.browserYT = &fakeYT{liked: fakeTracks("two", 2)}
	h.keys("A", "enter")
	if h.m.user != "sar1k" || len(h.m.lib.Liked) != 2 || h.m.lib.Liked[0].Title != "two song 0" {
		t.Fatalf("library of the previous account leaked: %v", titles(h.m.lib.Liked))
	}
}

// ---------- layout ----------

func TestRenderLoginEverySize(t *testing.T) {
	h := loginHarness(t, nil)
	sizes := [][2]int{{20, 5}, {50, 16}, {60, 20}, {80, 24}, {120, 40}, {250, 70}}
	resize := func() {
		for _, sz := range sizes {
			h.send(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
		}
	}
	resize() // chooser
	h.browserErr = errors.New("a very long error message that goes on and on and on and on and on")
	h.keys("enter")
	resize() // browser error
	h.keys("esc", "j", "j", "enter")
	resize() // oauth form
	h.keys("enter")
	resize() // form error
	h.m.stage = stageDevice
	h.send(deviceMsg{err: errors.New("a very long error message that goes on and on and on and on")})
	resize()
	h.send(deviceMsg{da: &oauth2.DeviceAuthResponse{UserCode: "ABCD-EFGH", VerificationURI: "https://www.google.com/device", Expiry: time.Now().Add(time.Minute)}})
	resize()
}

func TestRenderEverySizeAndState(t *testing.T) {
	h := newHarness(t, hopt{})
	yt := &fakeYT{liked: fakeTracks("liked with a very very long title that keeps going", 40),
		pls: []youtube.Playlist{{ID: "P", Title: strings.Repeat("Long playlist name ", 5), Count: 9}}}
	h.send(clientMsg{yt: yt, user: "a-user-with-a-long-name"})
	states := []func(){
		func() {},
		func() { h.keys("enter"); h.loaded() },
		func() { h.keys("2") },
		func() { h.keys("3") },
		func() { h.keys("1", "/") },
		func() { h.keys("esc", "f"); h.typeText("long") },
		func() { h.keys("esc", "?") },
		func() { h.keys("esc", "b") },
		func() { h.keys("esc", "v") },
		func() { h.keys("v") },
		func() { h.keys("z", "s", "r") },
	}
	sizes := [][2]int{{20, 5}, {49, 15}, {50, 16}, {55, 18}, {60, 20}, {76, 24}, {80, 24}, {99, 30}, {100, 31}, {140, 40}, {200, 60}, {300, 90}}
	for _, st := range states {
		st()
		for _, sz := range sizes {
			h.send(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]}) // check() validates the frame
		}
	}
}
