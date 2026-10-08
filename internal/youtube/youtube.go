// Package youtube wraps the official YouTube Data API v3 for the user's library.
package youtube

import (
	"context"
	"regexp"
	"strconv"
	"time"

	"github.com/sarvarbek-566/ytamp/internal/track"
	"golang.org/x/oauth2"
	"google.golang.org/api/option"
	yt "google.golang.org/api/youtube/v3"
)

// maxItems caps how many tracks we pull per list to protect the daily quota.
const maxItems = 1000

// Client is an authenticated YouTube Data API client.
type Client struct{ svc *yt.Service }

// Playlist is one of the user's playlists.
type Playlist struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Count int64  `json:"count"`
}

// New creates a client using the given token source.
func New(ctx context.Context, ts oauth2.TokenSource) (*Client, error) {
	svc, err := yt.NewService(ctx, option.WithTokenSource(ts))
	if err != nil {
		return nil, err
	}
	return &Client{svc: svc}, nil
}

// Me returns the signed-in channel's display name.
func (c *Client) Me(ctx context.Context) (string, error) {
	r, err := c.svc.Channels.List([]string{"snippet"}).Mine(true).Context(ctx).Do()
	if err != nil {
		return "", err
	}
	if len(r.Items) == 0 {
		return "YouTube", nil
	}
	return r.Items[0].Snippet.Title, nil
}

// Liked returns the user's liked videos, newest first (1 quota unit per 50).
func (c *Client) Liked(ctx context.Context) ([]track.Track, error) {
	var out []track.Track
	call := c.svc.Videos.List([]string{"snippet", "contentDetails"}).MyRating("like").MaxResults(50)
	err := call.Pages(ctx, func(r *yt.VideoListResponse) error {
		for _, v := range r.Items {
			t := fromVideo(v)
			t.Liked = true
			out = append(out, t)
		}
		if len(out) >= maxItems {
			return errStop
		}
		return nil
	})
	if err == errStop {
		err = nil
	}
	return out, err
}

// Playlists returns the user's own playlists.
func (c *Client) Playlists(ctx context.Context) ([]Playlist, error) {
	var out []Playlist
	call := c.svc.Playlists.List([]string{"snippet", "contentDetails"}).Mine(true).MaxResults(50)
	err := call.Pages(ctx, func(r *yt.PlaylistListResponse) error {
		for _, p := range r.Items {
			out = append(out, Playlist{ID: p.Id, Title: p.Snippet.Title, Count: p.ContentDetails.ItemCount})
		}
		return nil
	})
	return out, err
}

// PlaylistTracks returns a playlist's playable videos with durations.
func (c *Client) PlaylistTracks(ctx context.Context, id string) ([]track.Track, error) {
	var ids []string
	call := c.svc.PlaylistItems.List([]string{"contentDetails"}).PlaylistId(id).MaxResults(50)
	err := call.Pages(ctx, func(r *yt.PlaylistItemListResponse) error {
		for _, it := range r.Items {
			ids = append(ids, it.ContentDetails.VideoId)
		}
		if len(ids) >= maxItems {
			return errStop
		}
		return nil
	})
	if err != nil && err != errStop {
		return nil, err
	}
	return c.videos(ctx, ids)
}

// videos fetches details in batches of 50, preserving order and dropping
// private/deleted entries (which the API silently omits).
func (c *Client) videos(ctx context.Context, ids []string) ([]track.Track, error) {
	byID := make(map[string]track.Track, len(ids))
	for i := 0; i < len(ids); i += 50 {
		end := min(i+50, len(ids))
		r, err := c.svc.Videos.List([]string{"snippet", "contentDetails"}).Id(ids[i:end]...).Context(ctx).Do()
		if err != nil {
			return nil, err
		}
		for _, v := range r.Items {
			byID[v.Id] = fromVideo(v)
		}
	}
	out := make([]track.Track, 0, len(ids))
	for _, id := range ids {
		if t, ok := byID[id]; ok {
			out = append(out, t)
		}
	}
	return out, nil
}

// Rate likes (true) or removes the rating (false) of a video.
func (c *Client) Rate(ctx context.Context, id string, like bool) error {
	r := "none"
	if like {
		r = "like"
	}
	return c.svc.Videos.Rate(id, r).Context(ctx).Do()
}

// AddToPlaylist appends a video to one of the user's playlists.
func (c *Client) AddToPlaylist(ctx context.Context, playlistID, videoID string) error {
	_, err := c.svc.PlaylistItems.Insert([]string{"snippet"}, &yt.PlaylistItem{
		Snippet: &yt.PlaylistItemSnippet{
			PlaylistId: playlistID,
			ResourceId: &yt.ResourceId{Kind: "youtube#video", VideoId: videoID},
		},
	}).Context(ctx).Do()
	return err
}

type stopErr struct{}

func (stopErr) Error() string { return "stop" }

var errStop error = stopErr{}

func fromVideo(v *yt.Video) track.Track {
	artist, title := track.Clean(v.Snippet.Title, v.Snippet.ChannelTitle)
	var d time.Duration
	if v.ContentDetails != nil {
		d = ParseISODuration(v.ContentDetails.Duration)
	}
	return track.Track{ID: v.Id, Title: title, Artist: artist, Duration: d}
}

var isoDur = regexp.MustCompile(`^P(?:(\d+)D)?T?(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?$`)

// ParseISODuration parses YouTube's ISO-8601 durations like "PT3M50S".
func ParseISODuration(s string) time.Duration {
	m := isoDur.FindStringSubmatch(s)
	if m == nil {
		return 0
	}
	n := func(i int) time.Duration { v, _ := strconv.Atoi(m[i]); return time.Duration(v) }
	return n(1)*24*time.Hour + n(2)*time.Hour + n(3)*time.Minute + n(4)*time.Second
}
