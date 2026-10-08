// Package player drives a headless mpv process over its JSON IPC socket.
// mpv's built-in ytdl hook (yt-dlp) resolves and streams YouTube audio.
package player

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Event kinds emitted on Events().
type (
	// PropEvent is an observed property change.
	PropEvent struct {
		Name  string
		Value any // float64, bool, string or nil
	}
	// EndEvent fires when a file stops; Reason is eof|stop|quit|error|redirect.
	EndEvent struct {
		Reason string
		Err    string
	}
	// LoadedEvent fires once a file starts playing.
	LoadedEvent struct{}
	// ExitEvent fires if mpv dies unexpectedly.
	ExitEvent struct{ Err error }
)

// Player is a running mpv instance.
type Player struct {
	cmd     *exec.Cmd
	conn    net.Conn
	sock    string
	w       sync.Mutex
	nextID  atomic.Int64
	pmu     sync.Mutex
	pending map[int64]chan reply
	events  chan any
	closed  atomic.Bool
	exited  chan struct{}
	// mpv < 0.38 has no "index" argument in loadfile: options come third
	legacyLoad bool
}

type reply struct {
	Data  json.RawMessage
	Error string
}

// Options configure the mpv process.
type Options struct {
	SocketDir string
	Volume    int
	Quality   string   // best | medium | low
	Device    string   // optional mpv --audio-device
	Extra     []string // extra mpv flags (tests use --ao=null)
	Clients   string   // yt-dlp YouTube player clients (ytdl.DefaultClients)
	Cookies   string   // yt-dlp --cookies-from-browser value, optional
}

// Format returns the yt-dlp format selector for a quality setting.
func Format(q string) string {
	switch q {
	case "low":
		return "worstaudio[acodec=opus]/worstaudio/bestaudio/best"
	case "medium":
		return "bestaudio[abr<=130]/bestaudio/best"
	default:
		return "bestaudio[acodec=opus]/bestaudio/best"
	}
}

// Start launches mpv in idle mode and connects to its IPC socket.
func Start(o Options) (*Player, error) {
	if _, err := exec.LookPath("mpv"); err != nil {
		return nil, errors.New("mpv topilmadi (pacman -S mpv)")
	}
	if err := os.MkdirAll(o.SocketDir, 0o700); err != nil {
		return nil, err
	}
	sock := filepath.Join(o.SocketDir, "mpv-"+strconv.Itoa(os.Getpid())+".sock")
	_ = os.Remove(sock)

	cmd := exec.Command("mpv",
		"--no-config", // ignore user scripts (e.g. mpv-mpris) — ytamp owns MPRIS
		"--idle=yes", "--no-video", "--no-terminal", "--audio-display=no",
		"--input-ipc-server="+sock,
		"--audio-client-name=ytamp",
		"--ytdl=yes", "--ytdl-format="+Format(o.Quality),
		"--script-opts=ytdl_hook-ytdl_path=yt-dlp",
		"--ytdl-raw-options=no-playlist=,no-write-subs=,no-write-auto-subs=",
		"--sid=no", "--sub-auto=no",
		"--cache=yes", "--demuxer-max-bytes=64MiB", "--demuxer-readahead-secs=30",
		"--gapless-audio=weak",
		"--volume="+strconv.Itoa(o.Volume), "--volume-max=130",
	)
	if o.Clients != "" { // -append takes one pair, so commas in the value are safe
		cmd.Args = append(cmd.Args, "--ytdl-raw-options-append=extractor-args=youtube:player_client="+o.Clients)
	}
	if o.Cookies != "" {
		cmd.Args = append(cmd.Args, "--ytdl-raw-options-append=cookies-from-browser="+o.Cookies)
	}
	if o.Device != "" {
		cmd.Args = append(cmd.Args, "--audio-device="+o.Device)
	}
	cmd.Args = append(cmd.Args, o.Extra...)
	cmd.Stdout, cmd.Stderr = nil, nil
	// if ytamp dies for any reason (even SIGKILL), take mpv down with it
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mpv: %w", err)
	}

	var conn net.Conn
	var err error
	for i := 0; i < 100; i++ { // up to 5s
		if conn, err = net.Dial("unix", sock); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("mpv IPC ulanmadi: %w", err)
	}

	p := &Player{cmd: cmd, conn: conn, sock: sock, pending: map[int64]chan reply{}, events: make(chan any, 256), exited: make(chan struct{})}
	go p.readLoop()
	go func() {
		err := cmd.Wait()
		close(p.exited)
		if !p.closed.Load() {
			p.emit(ExitEvent{Err: err})
		}
	}()

	if d, err := p.call("get_property", "mpv-version"); err == nil {
		var v string
		if json.Unmarshal(d, &v) == nil {
			p.legacyLoad = legacyLoadfile(v)
		}
	}

	for i, prop := range []string{
		"time-pos", "duration", "pause", "volume", "mute", "idle-active",
		"paused-for-cache", "audio-codec-name", "audio-bitrate", "cache-buffering-state",
	} {
		p.async("observe_property", i+1, prop)
	}
	return p, nil
}

// Events returns the channel of player events.
func (p *Player) Events() <-chan any { return p.events }

func (p *Player) emit(e any) {
	select {
	case p.events <- e:
	default: // never block the reader; UI is already behind
	}
}

func (p *Player) readLoop() {
	sc := bufio.NewScanner(p.conn)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		var m struct {
			RequestID int64           `json:"request_id"`
			Error     string          `json:"error"`
			Data      json.RawMessage `json:"data"`
			Event     string          `json:"event"`
			Name      string          `json:"name"`
			Reason    string          `json:"reason"`
			FileError string          `json:"file_error"`
		}
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue
		}
		switch m.Event {
		case "":
			p.pmu.Lock()
			ch := p.pending[m.RequestID]
			delete(p.pending, m.RequestID)
			p.pmu.Unlock()
			if ch != nil {
				ch <- reply{Data: m.Data, Error: m.Error}
			}
		case "property-change":
			var v any
			if len(m.Data) > 0 {
				_ = json.Unmarshal(m.Data, &v)
			}
			p.emit(PropEvent{Name: m.Name, Value: v})
		case "end-file":
			p.emit(EndEvent{Reason: m.Reason, Err: m.FileError})
		case "file-loaded":
			p.emit(LoadedEvent{})
		}
	}
}

func (p *Player) write(v map[string]any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	p.w.Lock()
	defer p.w.Unlock()
	_ = p.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	_, err = p.conn.Write(append(b, '\n'))
	return err
}

// async sends a command without waiting for the reply.
func (p *Player) async(args ...any) {
	if p == nil || p.closed.Load() {
		return
	}
	_ = p.write(map[string]any{"command": args})
}

// call sends a command and waits (bounded) for its reply.
func (p *Player) call(args ...any) (json.RawMessage, error) {
	id := p.nextID.Add(1)
	ch := make(chan reply, 1)
	p.pmu.Lock()
	p.pending[id] = ch
	p.pmu.Unlock()
	if err := p.write(map[string]any{"command": args, "request_id": id}); err != nil {
		return nil, err
	}
	select {
	case r := <-ch:
		if r.Error != "" && r.Error != "success" {
			return nil, errors.New(r.Error)
		}
		return r.Data, nil
	case <-time.After(3 * time.Second):
		p.pmu.Lock()
		delete(p.pending, id)
		p.pmu.Unlock()
		return nil, errors.New("mpv javob bermadi")
	}
}

// Load replaces the current file and starts playing it at start seconds.
func (p *Player) Load(url string, start float64) {
	opts := "start=" + strconv.FormatFloat(start, 'f', 1, 64)
	p.loadfile(url, opts)
	p.async("set_property", "pause", false)
}

// LoadPaused loads a file but keeps it paused (session resume).
func (p *Player) LoadPaused(url string, start float64) {
	p.async("set_property", "pause", true)
	opts := "start=" + strconv.FormatFloat(start, 'f', 1, 64)
	p.loadfile(url, opts)
}

func (p *Player) loadfile(url, opts string) {
	if p == nil {
		return
	}
	if p.legacyLoad {
		p.async("loadfile", url, "replace", opts)
		return
	}
	p.async("loadfile", url, "replace", -1, opts)
}

// legacyLoadfile reports whether an "mpv-version" string ("mpv v0.37.0",
// "mpv 0.36.0-dirty", "mpv v0.41.0-123-gabc") predates loadfile's index
// argument (added in 0.38). Unparseable (e.g. git builds) → modern.
func legacyLoadfile(v string) bool {
	v = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(v), "mpv "), "v")
	maj, rest, ok := strings.Cut(v, ".")
	if !ok {
		return false
	}
	minS := rest
	if i := strings.IndexAny(rest, ".-+ "); i >= 0 {
		minS = rest[:i]
	}
	a, err1 := strconv.Atoi(maj)
	b, err2 := strconv.Atoi(minS)
	if err1 != nil || err2 != nil {
		return false
	}
	return a == 0 && b < 38
}

func (p *Player) TogglePause()        { p.async("cycle", "pause") }
func (p *Player) SetPause(b bool)     { p.async("set_property", "pause", b) }
func (p *Player) Seek(sec float64)    { p.async("seek", sec, "relative+exact") }
func (p *Player) SeekTo(sec float64)  { p.async("seek", sec, "absolute+exact") }
func (p *Player) SetVolume(v float64) { p.async("set_property", "volume", v) }
func (p *Player) ToggleMute()         { p.async("cycle", "mute") }
func (p *Player) Stop()               { p.async("stop") }

// Speed sets the playback speed (pitch-corrected).
func (p *Player) Speed(s float64) { p.async("set_property", "speed", s) }

// Position queries the current position synchronously.
func (p *Player) Position() (float64, error) {
	d, err := p.call("get_property", "time-pos")
	if err != nil {
		return 0, err
	}
	var f float64
	return f, json.Unmarshal(d, &f)
}

// Close shuts mpv down and removes the socket.
func (p *Player) Close() {
	if p == nil || p.closed.Swap(true) {
		return
	}
	_ = p.write(map[string]any{"command": []any{"quit"}}) // async() refuses once closed
	select {
	case <-p.exited:
	case <-time.After(1500 * time.Millisecond):
		_ = p.cmd.Process.Kill()
		select {
		case <-p.exited:
		case <-time.After(time.Second):
		}
	}
	_ = p.conn.Close()
	_ = os.Remove(p.sock)
}
