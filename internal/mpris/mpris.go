// Package mpris exposes ytamp on D-Bus as an MPRIS2 media player, so media
// keys, playerctl and desktop bars can control it.
package mpris

import (
	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
	"github.com/voidsecurity404/ytamp/internal/track"
)

const (
	busName   = "org.mpris.MediaPlayer2.ytamp"
	objPath   = "/org/mpris/MediaPlayer2"
	rootIface = "org.mpris.MediaPlayer2"
	playIface = "org.mpris.MediaPlayer2.Player"
)

// Handler receives control requests from D-Bus clients.
type Handler interface {
	MprisPlayPause()
	MprisPlay()
	MprisPause()
	MprisNext()
	MprisPrev()
	MprisStop()
	MprisSeek(offsetUs int64)
	MprisSetPosition(us int64)
	MprisQuit()
}

// Server is a running MPRIS endpoint.
type Server struct {
	conn  *dbus.Conn
	props *prop.Properties
}

type root struct{ h Handler }

func (r root) Raise() *dbus.Error { return nil }
func (r root) Quit() *dbus.Error  { r.h.MprisQuit(); return nil }

type player struct{ h Handler }

func (p player) Next() *dbus.Error      { p.h.MprisNext(); return nil }
func (p player) Previous() *dbus.Error  { p.h.MprisPrev(); return nil }
func (p player) Pause() *dbus.Error     { p.h.MprisPause(); return nil }
func (p player) PlayPause() *dbus.Error { p.h.MprisPlayPause(); return nil }
func (p player) Stop() *dbus.Error      { p.h.MprisStop(); return nil }
func (p player) Play() *dbus.Error      { p.h.MprisPlay(); return nil }
func (p player) Seek(x int64) *dbus.Error {
	p.h.MprisSeek(x)
	return nil
}
func (p player) SetPosition(_ dbus.ObjectPath, x int64) *dbus.Error {
	p.h.MprisSetPosition(x)
	return nil
}
func (p player) OpenUri(string) *dbus.Error { return nil }

// New registers ytamp on the session bus.
func New(h Handler) (*Server, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil, err
	}
	if reply, err := conn.RequestName(busName, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		conn.Close()
		if err == nil {
			err = dbus.ErrClosed
		}
		return nil, err
	}

	r, p := root{h}, player{h}
	_ = conn.Export(r, objPath, rootIface)
	_ = conn.Export(p, objPath, playIface)

	ro := func(v any) *prop.Prop { return &prop.Prop{Value: v, Writable: false, Emit: prop.EmitTrue} }
	props, err := prop.Export(conn, objPath, prop.Map{
		rootIface: {
			"CanQuit":             ro(true),
			"CanRaise":            ro(false),
			"HasTrackList":        ro(false),
			"Identity":            ro("ytamp"),
			"DesktopEntry":        ro("ytamp"),
			"SupportedUriSchemes": ro([]string{"https"}),
			"SupportedMimeTypes":  ro([]string{}),
		},
		playIface: {
			"PlaybackStatus": ro("Stopped"),
			"LoopStatus":     ro("None"),
			"Rate":           ro(1.0),
			"Shuffle":        ro(false),
			"Metadata":       ro(map[string]dbus.Variant{}),
			"Volume":         ro(1.0),
			"Position":       {Value: int64(0), Writable: false, Emit: prop.EmitFalse},
			"MinimumRate":    ro(1.0),
			"MaximumRate":    ro(1.0),
			"CanGoNext":      ro(true),
			"CanGoPrevious":  ro(true),
			"CanPlay":        ro(true),
			"CanPause":       ro(true),
			"CanSeek":        ro(true),
			"CanControl":     ro(true),
		},
	})
	if err != nil {
		conn.Close()
		return nil, err
	}

	node := &introspect.Node{
		Name: objPath,
		Interfaces: []introspect.Interface{
			introspect.IntrospectData,
			prop.IntrospectData,
			{Name: rootIface, Methods: introspect.Methods(r), Properties: props.Introspection(rootIface)},
			{Name: playIface, Methods: introspect.Methods(p), Properties: props.Introspection(playIface)},
		},
	}
	_ = conn.Export(introspect.NewIntrospectable(node), objPath, "org.freedesktop.DBus.Introspectable")
	return &Server{conn: conn, props: props}, nil
}

// SetTrack publishes the current track's metadata.
func (s *Server) SetTrack(t track.Track) {
	if s == nil {
		return
	}
	md := map[string]dbus.Variant{
		"mpris:trackid": dbus.MakeVariant(dbus.ObjectPath("/org/ytamp/track/" + sanitize(t.ID))),
		"mpris:length":  dbus.MakeVariant(t.Duration.Microseconds()),
		"mpris:artUrl":  dbus.MakeVariant(t.Thumb()),
		"xesam:title":   dbus.MakeVariant(t.Title),
		"xesam:artist":  dbus.MakeVariant([]string{t.Artist}),
		"xesam:url":     dbus.MakeVariant(t.URL()),
	}
	s.props.SetMust(playIface, "Metadata", md)
}

// SetStatus sets Playing | Paused | Stopped.
func (s *Server) SetStatus(st string) {
	if s != nil {
		s.props.SetMust(playIface, "PlaybackStatus", st)
	}
}

// SetPosition updates the (non-signalling) position property.
func (s *Server) SetPosition(us int64) {
	if s != nil {
		s.props.SetMust(playIface, "Position", us)
	}
}

// SetVolume publishes volume in 0..1.
func (s *Server) SetVolume(v float64) {
	if s != nil {
		s.props.SetMust(playIface, "Volume", v)
	}
}

// SetModes publishes shuffle and loop state.
func (s *Server) SetModes(shuffle bool, loop string) {
	if s != nil {
		s.props.SetMust(playIface, "Shuffle", shuffle)
		s.props.SetMust(playIface, "LoopStatus", loop)
	}
}

// Seeked emits the Seeked signal after a jump.
func (s *Server) Seeked(us int64) {
	if s != nil {
		_ = s.conn.Emit(objPath, playIface+".Seeked", us)
	}
}

// Close releases the bus name.
func (s *Server) Close() {
	if s != nil {
		_, _ = s.conn.ReleaseName(busName)
		s.conn.Close()
	}
}

// D-Bus object paths only allow [A-Za-z0-9_]; YouTube IDs may contain '-'.
func sanitize(id string) string {
	b := []byte(id)
	for i, c := range b {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			b[i] = '_'
		}
	}
	return string(b)
}
