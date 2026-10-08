package ytdl

import (
	"context"
	"os"
	"testing"
	"time"
)

// YTAMP_BROWSER=firefox go test ./internal/ytdl -run Browser -v
// Reads the signed-in library; with YTAMP_BROWSER_WRITE=1 also likes and
// un-likes a test video (restoring the original state).
func TestBrowserLibrary(t *testing.T) {
	b := os.Getenv("YTAMP_BROWSER")
	if b == "" {
		t.Skip("set YTAMP_BROWSER=firefox to test with a real browser session")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	c := NewBrowserClient(b)

	me, err := c.Me(ctx)
	if err != nil || me == "" {
		t.Fatalf("Me: %q %v", me, err)
	}
	start := time.Now()
	liked, err := c.Liked(ctx)
	if err != nil || len(liked) == 0 {
		t.Fatalf("Liked: %d %v", len(liked), err)
	}
	t.Logf("me=%q liked=%d (%.1fs)", me, len(liked), time.Since(start).Seconds())
	pls, err := c.Playlists(ctx)
	if err != nil {
		t.Fatalf("Playlists: %v", err)
	}
	for _, p := range pls {
		if p.ID == "LL" {
			t.Fatal("liked list should not be repeated as a playlist")
		}
	}
	t.Logf("playlists=%d", len(pls))
	if len(pls) > 0 {
		ts, err := c.PlaylistTracks(ctx, pls[0].ID)
		if err != nil {
			t.Fatalf("PlaylistTracks(%s): %v", pls[0].ID, err)
		}
		t.Logf("first playlist %q has %d tracks", pls[0].Title, len(ts))
	}

	if os.Getenv("YTAMP_BROWSER_WRITE") != "1" {
		return
	}
	const vid = "jNQXAC9IVRw" // "Me at the zoo"
	for _, tr := range liked {
		if tr.ID == vid {
			t.Skip("test video is already liked; not touching it")
		}
	}
	if err := c.Rate(ctx, vid, true); err != nil {
		t.Fatalf("like: %v", err)
	}
	defer func() {
		if err := c.Rate(context.Background(), vid, false); err != nil {
			t.Errorf("UNLIKE FAILED — please remove the like on %s manually: %v", vid, err)
		}
	}()
	time.Sleep(2 * time.Second)
	after, err := c.Liked(ctx)
	if err != nil || len(after) == 0 || after[0].ID != vid {
		t.Fatalf("liked video not at top of Liked list (err=%v)", err)
	}
	if err := c.Rate(ctx, vid, false); err != nil {
		t.Fatalf("unlike: %v", err)
	}
	time.Sleep(2 * time.Second)
	again, _ := c.Liked(ctx)
	if len(again) > 0 && again[0].ID == vid {
		t.Fatal("unlike did not take effect")
	}
}

func TestBrowserNotSignedIn(t *testing.T) {
	if os.Getenv("YTAMP_NET") == "" {
		t.Skip("network")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, err := NewBrowserClient("vivaldi").Me(ctx); err == nil {
		t.Fatal("a browser without a profile must fail")
	}
}

func TestBrowserAddToPlaylist(t *testing.T) {
	if os.Getenv("YTAMP_BROWSER") == "" || os.Getenv("YTAMP_BROWSER_WRITE") != "1" {
		t.Skip("needs YTAMP_BROWSER and YTAMP_BROWSER_WRITE=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	c := NewBrowserClient(os.Getenv("YTAMP_BROWSER"))
	// a throwaway private playlist, deleted at the end
	id, err := c.ops.createPlaylist(ctx, "ytamp test (o'chiriladi)")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer func() {
		if err := c.ops.deletePlaylist(context.Background(), id); err != nil {
			t.Errorf("DELETE FAILED — remove playlist %s manually: %v", id, err)
		}
	}()
	if err := c.AddToPlaylist(ctx, id, "jNQXAC9IVRw"); err != nil {
		t.Fatalf("add: %v", err)
	}
	time.Sleep(2 * time.Second)
	ts, err := c.PlaylistTracks(ctx, id)
	if err != nil || len(ts) != 1 || ts[0].ID != "jNQXAC9IVRw" {
		t.Fatalf("playlist content: %+v %v", ts, err)
	}
	if err := c.AddToPlaylist(ctx, "LL", "jNQXAC9IVRw"); err == nil {
		t.Fatal("adding to Liked must be refused")
	}
}
