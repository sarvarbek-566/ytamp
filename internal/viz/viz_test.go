package viz

import (
	"math"
	"math/cmplx"
	"testing"
)

func TestFFTFindsTone(t *testing.T) {
	const n = 1024
	x := make([]complex128, n)
	for i := range x {
		x[i] = complex(math.Sin(2*math.Pi*50*float64(i)/n), 0) // bin 50
	}
	fft(x)
	best := 0
	for i := 1; i < n/2; i++ {
		if cmplx.Abs(x[i]) > cmplx.Abs(x[best]) {
			best = i
		}
	}
	if best != 50 {
		t.Fatalf("peak at bin %d, want 50", best)
	}
}

func TestBarsSilenceAndRange(t *testing.T) {
	a := New()
	a.live = true
	for _, v := range a.Bars(32, true, 0) {
		if v != 0 {
			t.Fatalf("silence produced bar %v", v)
		}
	}
	for i := range a.ring {
		a.ring[i] = 0.5 * math.Sin(2*math.Pi*440*float64(i)/rate)
	}
	for k := 0; k < 10; k++ {
		for _, v := range a.Bars(32, true, 0) {
			if v < 0 || v > 1 || math.IsNaN(v) {
				t.Fatalf("bar out of range: %v", v)
			}
		}
	}
}
