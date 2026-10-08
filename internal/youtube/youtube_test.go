package youtube

import (
	"testing"
	"time"
)

func TestParseISODuration(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"PT3M50S": 3*time.Minute + 50*time.Second,
		"PT1H2M":  time.Hour + 2*time.Minute,
		"PT45S":   45 * time.Second,
		"P1DT1S":  24*time.Hour + time.Second,
		"P0D":     0,
		"bogus":   0,
	} {
		if got := ParseISODuration(in); got != want {
			t.Errorf("ParseISODuration(%q) = %v, want %v", in, got, want)
		}
	}
}
