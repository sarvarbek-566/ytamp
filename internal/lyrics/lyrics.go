// Package lyrics fetches time-synced lyrics from lrclib.net (free, no key).
package lyrics

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Line is one lyric line; At is zero for unsynced lyrics.
type Line struct {
	At   time.Duration
	Text string
}

// Lyrics holds a song's lines.
type Lyrics struct {
	Lines  []Line
	Synced bool
}

// ErrNotFound means lrclib has nothing for this track.
var ErrNotFound = errors.New("matn topilmadi")

var client = &http.Client{Timeout: 8 * time.Second}

type result struct {
	TrackName    string  `json:"trackName"`
	ArtistName   string  `json:"artistName"`
	Duration     float64 `json:"duration"`
	Instrumental bool    `json:"instrumental"`
	PlainLyrics  string  `json:"plainLyrics"`
	SyncedLyrics string  `json:"syncedLyrics"`
}

// Fetch tries an exact match first, then a fuzzy search picking the result
// whose duration is closest to the track's.
func Fetch(ctx context.Context, artist, title string, dur time.Duration) (*Lyrics, error) {
	q := url.Values{"artist_name": {artist}, "track_name": {title}}
	if dur > 0 {
		q.Set("duration", strconv.Itoa(int(dur.Seconds())))
	}
	var r result
	if err := get(ctx, "https://lrclib.net/api/get?"+q.Encode(), &r); err == nil && (r.SyncedLyrics != "" || r.PlainLyrics != "") {
		return parse(r), nil
	}

	var rs []result
	if err := get(ctx, "https://lrclib.net/api/search?"+url.Values{"q": {artist + " " + title}}.Encode(), &rs); err != nil {
		return nil, err
	}
	best, bestScore := -1, 1e18
	for i, c := range rs {
		if c.SyncedLyrics == "" && c.PlainLyrics == "" {
			continue
		}
		score := 0.0
		if dur > 0 {
			d := c.Duration - dur.Seconds()
			score = d * d
		}
		if c.SyncedLyrics == "" {
			score += 400 // prefer synced
		}
		if score < bestScore {
			best, bestScore = i, score
		}
	}
	if best < 0 {
		return nil, ErrNotFound
	}
	return parse(rs[best]), nil
}

// get fetches JSON, retrying once on server errors (lrclib returns the
// occasional 502/503).
func get(ctx context.Context, u string, v any) error {
	err := getOnce(ctx, u, v)
	var se serverErr
	if errors.As(err, &se) && ctx.Err() == nil {
		select {
		case <-time.After(700 * time.Millisecond):
		case <-ctx.Done():
			return err
		}
		err = getOnce(ctx, u, v)
	}
	return err
}

type serverErr struct{ status string }

func (e serverErr) Error() string { return "lrclib: " + e.status }

func getOnce(ctx context.Context, u string, v any) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("User-Agent", "ytamp (https://github.com/voidsecurity404/ytamp)")
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if res.StatusCode >= 500 {
		return serverErr{res.Status}
	}
	if res.StatusCode != http.StatusOK {
		return errors.New("lrclib: " + res.Status)
	}
	return json.NewDecoder(res.Body).Decode(v)
}

var stamp = regexp.MustCompile(`\[(\d+):(\d+(?:\.\d+)?)\]`)

func parse(r result) *Lyrics {
	if r.SyncedLyrics == "" {
		l := &Lyrics{}
		for _, s := range strings.Split(r.PlainLyrics, "\n") {
			l.Lines = append(l.Lines, Line{Text: strings.TrimSpace(s)})
		}
		return l
	}
	l := &Lyrics{Synced: true}
	for _, raw := range strings.Split(r.SyncedLyrics, "\n") {
		ms := stamp.FindAllStringSubmatch(raw, -1)
		text := strings.TrimSpace(stamp.ReplaceAllString(raw, ""))
		for _, m := range ms {
			min, _ := strconv.Atoi(m[1])
			sec, _ := strconv.ParseFloat(m[2], 64)
			at := time.Duration(min)*time.Minute + time.Duration(sec*float64(time.Second))
			l.Lines = append(l.Lines, Line{At: at, Text: text})
		}
	}
	sort.SliceStable(l.Lines, func(i, j int) bool { return l.Lines[i].At < l.Lines[j].At })
	return l
}

// Index returns the line active at pos (-1 before the first line).
func (l *Lyrics) Index(pos time.Duration) int {
	if l == nil || !l.Synced {
		return -1
	}
	return sort.Search(len(l.Lines), func(i int) bool { return l.Lines[i].At > pos }) - 1
}
