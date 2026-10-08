package ytdl

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	origin        = "https://www.youtube.com"
	clientVersion = "2.20260801.01.00"
)

// innertube performs account actions (like, add to playlist) with the
// browser session, signing requests the way youtube.com itself does.
type innertube struct {
	browser string
	mu      sync.Mutex
	cookies map[string]string
	loaded  time.Time
	http    *http.Client
}

// loadCookies asks yt-dlp to export the browser's YouTube cookies into a
// private temp file, reads them and deletes the file straight away.
func (it *innertube) loadCookies(ctx context.Context, force bool) (map[string]string, error) {
	it.mu.Lock()
	defer it.mu.Unlock()
	if !force && it.cookies != nil && time.Since(it.loaded) < 15*time.Minute {
		return it.cookies, nil
	}
	dir, err := os.MkdirTemp("", "ytamp-ck-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	jar := filepath.Join(dir, "c.txt")
	_, err = exe(ctx, "--cookies-from-browser", it.browser, "--cookies", jar, "--skip-download",
		"--flat-playlist", "--playlist-end", "1", "-q", "--no-warnings", "https://www.youtube.com/playlist?list=WL")
	if err != nil {
		return nil, browserErr(err)
	}
	f, err := os.Open(jar)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	ck := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimPrefix(sc.Text(), "#HttpOnly_")
		if strings.HasPrefix(line, "#") {
			continue
		}
		p := strings.Split(line, "\t")
		if len(p) == 7 && strings.HasSuffix(p[0], "youtube.com") {
			ck[p[5]] = p[6]
		}
	}
	if ck["SAPISID"] == "" && ck["__Secure-3PAPISID"] == "" {
		return nil, ErrNotSignedIn
	}
	it.cookies, it.loaded = ck, time.Now()
	return ck, nil
}

// authorization builds the SAPISIDHASH header youtube.com sends.
func authorization(ck map[string]string, now time.Time) string {
	ts := strconv.FormatInt(now.Unix(), 10)
	var parts []string
	for _, s := range []struct{ scheme, cookie string }{
		{"SAPISIDHASH", "SAPISID"}, {"SAPISID1PHASH", "__Secure-1PAPISID"}, {"SAPISID3PHASH", "__Secure-3PAPISID"},
	} {
		v := ck[s.cookie]
		if s.cookie == "SAPISID" && v == "" {
			v = ck["__Secure-3PAPISID"]
		}
		if v == "" {
			continue
		}
		h := sha1.Sum([]byte(ts + " " + v + " " + origin))
		parts = append(parts, s.scheme+" "+ts+"_"+hex.EncodeToString(h[:]))
	}
	return strings.Join(parts, " ")
}

func cookieHeader(ck map[string]string) string {
	var b strings.Builder
	for k, v := range ck {
		if b.Len() > 0 {
			b.WriteString("; ")
		}
		b.WriteString(k + "=" + v)
	}
	return b.String()
}

// call POSTs to an InnerTube endpoint, refreshing cookies once on 401/403.
func (it *innertube) call(ctx context.Context, endpoint string, body map[string]any) error {
	return it.callJSON(ctx, endpoint, body, nil)
}

// callJSON is call that also decodes the response into out (if non-nil).
func (it *innertube) callJSON(ctx context.Context, endpoint string, body map[string]any, out any) error {
	body["context"] = map[string]any{"client": map[string]any{"clientName": "WEB", "clientVersion": clientVersion, "hl": "en"}}
	payload, _ := json.Marshal(body)
	if it.http == nil {
		it.http = &http.Client{Timeout: 15 * time.Second}
	}
	for attempt := 0; attempt < 2; attempt++ {
		ck, err := it.loadCookies(ctx, attempt > 0)
		if err != nil {
			return err
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
			origin+"/youtubei/v1/"+endpoint+"?prettyPrint=false", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", authorization(ck, time.Now()))
		req.Header.Set("Cookie", cookieHeader(ck))
		req.Header.Set("Origin", origin)
		req.Header.Set("X-Origin", origin)
		req.Header.Set("X-Goog-AuthUser", "0")
		req.Header.Set("X-Youtube-Client-Name", "1")
		req.Header.Set("X-Youtube-Client-Version", clientVersion)
		req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:140.0) Gecko/20100101 Firefox/140.0")
		res, err := it.http.Do(req)
		if err != nil {
			return err
		}
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		res.Body.Close()
		switch {
		case res.StatusCode == http.StatusOK:
			if out != nil {
				return json.Unmarshal(msg, out)
			}
			return nil
		case (res.StatusCode == 401 || res.StatusCode == 403) && attempt == 0:
			continue // stale cookies: re-export and retry once
		case res.StatusCode == 401 || res.StatusCode == 403:
			return ErrNotSignedIn
		default:
			var e struct {
				Error struct{ Message string } `json:"error"`
			}
			_ = json.Unmarshal(msg, &e)
			if e.Error.Message != "" {
				return errors.New("YouTube: " + e.Error.Message)
			}
			return fmt.Errorf("YouTube: %s", res.Status)
		}
	}
	return ErrNotSignedIn
}

func (it *innertube) rate(ctx context.Context, id string, like bool) error {
	ep := "like/removelike"
	if like {
		ep = "like/like"
	}
	return it.call(ctx, ep, map[string]any{"target": map[string]any{"videoId": id}})
}

func (it *innertube) addToPlaylist(ctx context.Context, playlistID, videoID string) error {
	if playlistID == "LL" || strings.HasPrefix(playlistID, "RD") {
		return errors.New("bu ro'yxatga qo'lda qo'shib bo'lmaydi")
	}
	return it.call(ctx, "browse/edit_playlist", map[string]any{
		"playlistId": playlistID,
		"actions":    []map[string]any{{"action": "ACTION_ADD_VIDEO", "addedVideoId": videoID}},
	})
}

// createPlaylist / deletePlaylist exist for the integration tests, which
// need a throwaway playlist so they never touch the user's own lists.
func (it *innertube) createPlaylist(ctx context.Context, title string) (string, error) {
	var out struct {
		PlaylistID string `json:"playlistId"`
	}
	if err := it.callJSON(ctx, "playlist/create", map[string]any{"title": title, "privacyStatus": "PRIVATE"}, &out); err != nil {
		return "", err
	}
	if out.PlaylistID == "" {
		return "", errors.New("YouTube playlist id qaytarmadi")
	}
	return out.PlaylistID, nil
}

func (it *innertube) deletePlaylist(ctx context.Context, id string) error {
	return it.call(ctx, "playlist/delete", map[string]any{"playlistId": id})
}
