package ytdl

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func needNet(t *testing.T) {
	if os.Getenv("YTAMP_NET") == "" {
		t.Skip("set YTAMP_NET=1 for network tests")
	}
}

func TestSearchRadioResolveStream(t *testing.T) {
	needNet(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	ts, err := Search(ctx, "the weeknd starboy", 5)
	if err != nil || len(ts) == 0 {
		t.Fatalf("search: %v (%d)", err, len(ts))
	}
	for _, tr := range ts {
		if len(tr.ID) != 11 || tr.Title == "" || tr.Artist == "" {
			t.Fatalf("bad track %+v", tr)
		}
	}
	if !strings.Contains(strings.ToLower(ts[0].Title), "starboy") {
		t.Errorf("top hit %q", ts[0].Title)
	}

	r, err := Radio(ctx, ts[0].ID, 10)
	if err != nil || len(r) < 5 {
		t.Fatalf("radio: %v (%d)", err, len(r))
	}
	for _, tr := range r {
		if tr.ID == ts[0].ID {
			t.Fatal("radio contains its seed")
		}
	}

	one, err := Resolve(ctx, "https://youtu.be/"+ts[0].ID)
	if err != nil || one.ID != ts[0].ID || one.Duration == 0 {
		t.Fatalf("resolve: %+v %v", one, err)
	}

	u, err := StreamURL(ctx, ts[0].ID, "bestaudio")
	if err != nil || !strings.HasPrefix(u, "https://") {
		t.Fatalf("stream url: %q %v", u, err)
	}

	if _, err := Search(ctx, "zxqv"+strings.Repeat("k", 40), 3); err != nil {
		t.Fatalf("no-result search should not error: %v", err)
	}
	if _, err := Resolve(ctx, "https://youtu.be/AAAAAAAAAAA"); err == nil {
		t.Fatal("resolving a bogus id should fail")
	}
}
