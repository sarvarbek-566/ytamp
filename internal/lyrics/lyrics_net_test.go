package lyrics

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestFetchReal(t *testing.T) {
	if os.Getenv("YTAMP_NET") == "" {
		t.Skip("set YTAMP_NET=1 for network tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	l, err := Fetch(ctx, "The Weeknd", "Blinding Lights", 200*time.Second)
	if err != nil || !l.Synced || len(l.Lines) < 10 {
		t.Fatalf("exact: %v %+v", err, l)
	}
	if _, err := Fetch(ctx, "Nobody Zzqx", "No Such Song Qwxz 12345", 0); err != ErrNotFound {
		t.Fatalf("missing song: want ErrNotFound, got %v", err)
	}
}
