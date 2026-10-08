package player

import (
	"encoding/binary"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// writeWAV writes a mono 16-bit sine of the given length.
func writeWAV(t *testing.T, path string, secs float64) {
	t.Helper()
	const rate = 8000
	n := int(secs * rate)
	data := make([]byte, 44+2*n)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:], uint32(36+2*n))
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1)
	binary.LittleEndian.PutUint16(data[22:], 1)
	binary.LittleEndian.PutUint32(data[24:], rate)
	binary.LittleEndian.PutUint32(data[28:], rate*2)
	binary.LittleEndian.PutUint16(data[32:], 2)
	binary.LittleEndian.PutUint16(data[34:], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], uint32(2*n))
	for i := 0; i < n; i++ {
		v := int16(8000 * math.Sin(2*math.Pi*440*float64(i)/rate))
		binary.LittleEndian.PutUint16(data[44+2*i:], uint16(v))
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

type watcher struct {
	t    *testing.T
	p    *Player
	pos  float64
	paus bool
	idle bool
}

// waitFor consumes events until cond is true or the timeout hits.
func (w *watcher) waitFor(what string, cond func(ev any) bool) any {
	w.t.Helper()
	deadline := time.After(8 * time.Second)
	for {
		select {
		case ev := <-w.p.Events():
			if pe, ok := ev.(PropEvent); ok {
				switch pe.Name {
				case "time-pos":
					if f, ok := pe.Value.(float64); ok {
						w.pos = f
					}
				case "pause":
					w.paus, _ = pe.Value.(bool)
				case "idle-active":
					w.idle, _ = pe.Value.(bool)
				}
			}
			if cond(ev) {
				return ev
			}
		case <-deadline:
			w.t.Fatalf("timed out waiting for %s (pos=%.2f pause=%v idle=%v)", what, w.pos, w.paus, w.idle)
		}
	}
}

func TestMPVIntegration(t *testing.T) {
	if _, err := exec.LookPath("mpv"); err != nil {
		t.Skip("mpv not installed")
	}
	dir := t.TempDir()
	long, short := filepath.Join(dir, "long.wav"), filepath.Join(dir, "short.wav")
	writeWAV(t, long, 20)
	writeWAV(t, short, 1)

	p, err := Start(Options{SocketDir: dir, Volume: 50, Extra: []string{"--ao=null"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close) // never leak mpv when an assertion fails
	w := &watcher{t: t, p: p}

	p.Load(long, 0)
	w.waitFor("file-loaded", func(ev any) bool { _, ok := ev.(LoadedEvent); return ok })
	w.waitFor("playback to advance", func(any) bool { return w.pos > 0.3 })

	p.TogglePause()
	w.waitFor("pause=true", func(any) bool { return w.paus })
	p.SeekTo(10)
	w.waitFor("seek to 10s", func(any) bool { return w.pos >= 9.9 && w.pos < 11 })
	p.Seek(-5)
	w.waitFor("relative seek", func(any) bool { return w.pos >= 4.3 && w.pos < 6 })
	if pos, err := p.Position(); err != nil || pos < 4.3 || pos > 6 {
		t.Fatalf("Position() = %v, %v", pos, err)
	}
	p.SetVolume(80)
	w.waitFor("volume 80", func(ev any) bool {
		pe, ok := ev.(PropEvent)
		return ok && pe.Name == "volume" && pe.Value == 80.0
	})
	p.ToggleMute()
	w.waitFor("mute", func(ev any) bool {
		pe, ok := ev.(PropEvent)
		return ok && pe.Name == "mute" && pe.Value == true
	})

	// replacing a file reports "stop" for the old one, never "eof"
	p.Load(short, 0)
	w.waitFor("old file stopped", func(ev any) bool {
		e, ok := ev.(EndEvent)
		if ok && e.Reason == "eof" {
			t.Fatal("replace reported eof")
		}
		return ok && e.Reason == "stop"
	})
	w.waitFor("short file unpaused", func(any) bool { return !w.paus })
	w.waitFor("natural eof", func(ev any) bool { e, ok := ev.(EndEvent); return ok && e.Reason == "eof" })
	w.waitFor("idle after eof", func(any) bool { return w.idle })

	// resume-style load: paused at an offset
	p.LoadPaused(long, 7)
	w.waitFor("paused load", func(ev any) bool { _, ok := ev.(LoadedEvent); return ok })
	w.waitFor("paused at 7s", func(any) bool { return w.paus && w.pos >= 6.9 && w.pos < 7.5 })
	time.Sleep(400 * time.Millisecond)
	if pos, _ := p.Position(); pos > 7.5 {
		t.Fatalf("paused load kept playing: %.2f", pos)
	}

	p.Load(filepath.Join(dir, "missing.wav"), 0)
	w.waitFor("error for missing file", func(ev any) bool { e, ok := ev.(EndEvent); return ok && e.Reason == "error" })

	p.Stop()
	start := time.Now()
	p.Close()
	if d := time.Since(start); d > time.Second {
		t.Fatalf("Close took %v — mpv ignored quit and was killed", d)
	}
	p.Close() // idempotent
	p.TogglePause()
	if _, err := os.Stat(p.sock); !os.IsNotExist(err) {
		t.Fatal("socket not removed")
	}
	if p.cmd.ProcessState == nil {
		t.Fatal("mpv still running after Close")
	}
}

func TestMPVCrashIsReported(t *testing.T) {
	if _, err := exec.LookPath("mpv"); err != nil {
		t.Skip("mpv not installed")
	}
	p, err := Start(Options{SocketDir: t.TempDir(), Extra: []string{"--ao=null"}})
	if err != nil {
		t.Fatal(err)
	}
	_ = p.cmd.Process.Kill()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-p.Events():
			if _, ok := ev.(ExitEvent); ok {
				p.Close()
				return
			}
		case <-deadline:
			t.Fatal("no ExitEvent after mpv died")
		}
	}
}

func TestMPVStreamsYouTube(t *testing.T) {
	if os.Getenv("YTAMP_NET") == "" {
		t.Skip("set YTAMP_NET=1 for network tests")
	}
	p, err := Start(Options{SocketDir: t.TempDir(), Volume: 0, Extra: []string{"--ao=null"}, Clients: "default,web_embedded"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	w := &watcher{t: t, p: p}
	p.Load("https://www.youtube.com/watch?v=4NRXx6U8ABQ", 0)
	w.waitFor("stream to load", func(ev any) bool {
		if e, ok := ev.(EndEvent); ok && e.Reason == "error" {
			t.Fatalf("stream failed: %s", e.Err)
		}
		_, ok := ev.(LoadedEvent)
		return ok
	})
	w.waitFor("audio to play", func(any) bool { return w.pos > 0.5 })
}

func TestMPVDiesWithParent(t *testing.T) {
	if os.Getenv("YTAMP_CHILD") == "1" { // the doomed parent
		p, err := Start(Options{SocketDir: os.Getenv("YTAMP_DIR"), Extra: []string{"--ao=null"}})
		if err != nil {
			os.Exit(2)
		}
		os.WriteFile(filepath.Join(os.Getenv("YTAMP_DIR"), "pid"), []byte(strconv.Itoa(p.cmd.Process.Pid)), 0o644)
		time.Sleep(time.Minute)
		return
	}
	if _, err := exec.LookPath("mpv"); err != nil {
		t.Skip("mpv not installed")
	}
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestMPVDiesWithParent$")
	cmd.Env = append(os.Environ(), "YTAMP_CHILD=1", "YTAMP_DIR="+dir)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var pid int
	for i := 0; i < 100 && pid == 0; i++ {
		time.Sleep(50 * time.Millisecond)
		b, _ := os.ReadFile(filepath.Join(dir, "pid"))
		pid, _ = strconv.Atoi(string(b))
	}
	if pid == 0 {
		t.Fatal("child never started mpv")
	}
	_ = cmd.Process.Kill() // SIGKILL: no cleanup code runs in the parent
	_ = cmd.Wait()
	for i := 0; i < 60; i++ {
		if syscall.Kill(pid, 0) != nil {
			return // mpv is gone
		}
		time.Sleep(50 * time.Millisecond)
	}
	syscall.Kill(pid, syscall.SIGKILL)
	t.Fatal("mpv survived its parent being SIGKILLed")
}

func TestLegacyLoadfileDetection(t *testing.T) {
	for v, want := range map[string]bool{
		"mpv v0.37.0": true, "mpv 0.36.0-dirty": true, "mpv v0.35.1": true,
		"mpv v0.38.0": false, "mpv v0.41.0": false, "mpv v0.41.0-123-gabcdef": false,
		"mpv 1.0.0": false, "mpv git-2024": false, "": false,
	} {
		if got := legacyLoadfile(v); got != want {
			t.Errorf("legacyLoadfile(%q) = %v, want %v", v, got, want)
		}
	}
}
