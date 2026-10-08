// Package viz captures the system audio monitor and turns it into a smoothed,
// log-frequency spectrum for the terminal visualizer.
package viz

import (
	"bufio"
	"encoding/binary"
	"io"
	"math"
	"math/cmplx"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

const (
	rate    = 44100
	fftSize = 2048
	fMin    = 40.0
	fMax    = 12000.0
	// noiseFloor stops auto-gain from inflating silence/hiss into tall bars;
	// a full-scale tone peaks around 500 with this window and size.
	noiseFloor = 4.0
)

// Analyzer reads PCM from the default sink monitor and computes bar levels.
type Analyzer struct {
	mu      sync.Mutex
	ring    [fftSize]float64
	pos     int
	live    bool
	cmd     *exec.Cmd
	bars    []float64
	peak    float64 // auto-gain reference
	window  [fftSize]float64
	lastTry time.Time
	gain    float64 // undoes mpv's software volume so bars don't shrink when quiet
}

// New creates an analyzer; call Start to begin capturing.
func New() *Analyzer {
	a := &Analyzer{peak: noiseFloor, gain: 1}
	for i := range a.window { // Hann
		a.window[i] = 0.5 * (1 - math.Cos(2*math.Pi*float64(i)/float64(fftSize-1)))
	}
	return a
}

// Ensure starts capturing if not already running. It prefers pw-record
// targeted at ytamp's own PipeWire stream (so other apps never leak into the
// visualizer); without pw-record it falls back to the default sink monitor.
// Safe to call often: retries are rate-limited. Returns whether it's live.
func (a *Analyzer) Ensure() bool {
	a.mu.Lock()
	if a.live || time.Since(a.lastTry) < 2*time.Second {
		live := a.live
		a.mu.Unlock()
		return live
	}
	a.lastTry = time.Now()
	a.mu.Unlock()

	var cmd *exec.Cmd
	if p, err := exec.LookPath("pw-record"); err == nil {
		cmd = exec.Command(p, "--target", Target, "--raw", "--rate", "44100", "--channels", "1",
			"--format", "s16", "--latency", "20ms",
			"-P", "{ node.dont-fallback = true, node.dont-reconnect = true, node.passive = true, node.name = ytamp-viz }", "-")
	} else if p, err := exec.LookPath("parec"); err == nil {
		cmd = exec.Command(p, "-d", "@DEFAULT_MONITOR@", "--raw", "--format=s16le",
			"--rate=44100", "--channels=1", "--latency-msec=25", "--client-name=ytamp-viz")
	} else {
		return false
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
	out, err := cmd.StdoutPipe()
	if err != nil || cmd.Start() != nil {
		return false
	}
	a.mu.Lock()
	a.cmd, a.live = cmd, true
	a.mu.Unlock()
	go func() {
		a.read(out)
		_ = cmd.Wait()
	}()
	return true
}

// Target is the PipeWire node name of ytamp's mpv stream (--audio-client-name).
const Target = "ytamp"

func (a *Analyzer) read(r io.Reader) {
	br := bufio.NewReaderSize(r, 4096)
	buf := make([]byte, 1024)
	for {
		n, err := io.ReadFull(br, buf)
		if err != nil {
			a.mu.Lock()
			a.live = false
			a.mu.Unlock()
			return
		}
		a.mu.Lock()
		for i := 0; i+1 < n; i += 2 {
			a.ring[a.pos] = float64(int16(binary.LittleEndian.Uint16(buf[i:]))) / 32768
			a.pos = (a.pos + 1) % fftSize
		}
		a.mu.Unlock()
	}
}

// SetVolume tells the analyzer mpv's volume (0-130) so it can compensate;
// mpv's volume curve is cubic.
func (a *Analyzer) SetVolume(v float64) {
	g := math.Pow(v/100, 3)
	a.mu.Lock()
	a.gain = 1 / math.Max(g, 0.01)
	a.mu.Unlock()
}

// Live reports whether real audio capture is running.
func (a *Analyzer) Live() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.live
}

// Stop kills the capture process.
func (a *Analyzer) Stop() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cmd != nil && a.cmd.Process != nil {
		_ = a.cmd.Process.Kill()
	}
}

// Bars returns n levels in 0..1. When playing is false bars fall to zero
// with gravity, giving a nice settle animation on pause.
func (a *Analyzer) Bars(n int, playing bool, t float64) []float64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.bars) != n {
		a.bars = make([]float64, n)
	}
	target := make([]float64, n)
	switch {
	case !playing:
	case a.live:
		a.spectrum(target)
	default:
		synthetic(target, t)
	}
	for i := range a.bars {
		if target[i] > a.bars[i] {
			a.bars[i] += (target[i] - a.bars[i]) * 0.65 // fast attack
		} else {
			a.bars[i] -= math.Max(0.04, (a.bars[i]-target[i])*0.22) // gravity
			a.bars[i] = math.Max(a.bars[i], target[i])
		}
	}
	return append([]float64(nil), a.bars...)
}

func (a *Analyzer) spectrum(out []float64) {
	x := make([]complex128, fftSize)
	for i := 0; i < fftSize; i++ {
		x[i] = complex(a.ring[(a.pos+i)%fftSize]*a.window[i]*a.gain, 0)
	}
	fft(x)
	n := len(out)
	binHz := float64(rate) / fftSize
	frameMax := 0.0
	for b := 0; b < n; b++ {
		lo := fMin * math.Pow(fMax/fMin, float64(b)/float64(n))
		hi := fMin * math.Pow(fMax/fMin, float64(b+1)/float64(n))
		i0, i1 := int(lo/binHz), int(hi/binHz)
		if i1 <= i0 {
			i1 = i0 + 1
		}
		sum := 0.0
		for i := i0; i < i1 && i < fftSize/2; i++ {
			sum = math.Max(sum, cmplx.Abs(x[i]))
		}
		// pink-noise tilt: music loses ~3-4.5dB/octave, so lift the highs
		fc := math.Sqrt(lo * hi)
		sum *= math.Pow(fc/150, 0.6)
		out[b] = sum
		frameMax = math.Max(frameMax, sum)
	}
	// slow auto-gain so quiet and loud tracks both fill the panel
	if frameMax > a.peak {
		a.peak = frameMax
	} else {
		a.peak = math.Max(a.peak*0.997, noiseFloor)
	}
	// dB scale across a 42dB window feels far more alive than linear
	for i := range out {
		if out[i] <= 0 {
			out[i] = 0
			continue
		}
		db := 20 * math.Log10(out[i]/a.peak)
		out[i] = math.Max(0, math.Min(1, 1+db/42))
	}
}

// synthetic is a pleasant fallback when the monitor can't be captured.
func synthetic(out []float64, t float64) {
	n := float64(len(out))
	beat := math.Pow(math.Abs(math.Sin(t*math.Pi*2.0)), 6)
	for i := range out {
		x := float64(i) / n
		v := 0.45*math.Exp(-x*2.2) + 0.25*math.Abs(math.Sin(x*9+t*3.1)) +
			0.15*math.Abs(math.Sin(x*23-t*5.3)) + 0.3*beat*math.Exp(-x*4)
		out[i] = math.Min(1, v)
	}
}

// fft is an in-place iterative radix-2 Cooley–Tukey FFT.
func fft(a []complex128) {
	n := len(a)
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			a[i], a[j] = a[j], a[i]
		}
	}
	for size := 2; size <= n; size <<= 1 {
		w := cmplx.Exp(complex(0, -2*math.Pi/float64(size)))
		for start := 0; start < n; start += size {
			wn := complex(1, 0)
			for k := 0; k < size/2; k++ {
				u, v := a[start+k], a[start+k+size/2]*wn
				a[start+k], a[start+k+size/2] = u+v, u-v
				wn *= w
			}
		}
	}
}
