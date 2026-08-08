package yt

import (
	"context"
	"net/http"

	"google.golang.org/api/option"
	ytapi "google.golang.org/api/youtube/v3"
)

type Client struct {
	svc *ytapi.Service
}

// New builds a Client around hc (an authenticated *http.Client in production).
// Tests pass option.WithEndpoint to aim the generated client at a fake server.
func New(ctx context.Context, hc *http.Client, opts ...option.ClientOption) (*Client, error) {
	opts = append([]option.ClientOption{option.WithHTTPClient(hc)}, opts...)
	svc, err := ytapi.NewService(ctx, opts...)
	if err != nil {
		return nil, err
	}
	return &Client{svc: svc}, nil
}

type Playlist struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Privacy     string `json:"privacy"`
	ItemCount   int64  `json:"item_count"`
	URL         string `json:"url"`
}

type PlaylistItem struct {
	ID       string `json:"playlist_item_id"`
	VideoID  string `json:"video_id"`
	Title    string `json:"title"`
	Position int64  `json:"position"`
}

type SearchResult struct {
	VideoID     string `json:"video_id"`
	Title       string `json:"title"`
	Channel     string `json:"channel"`
	PublishedAt string `json:"published_at"`
}

type Video struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Channel     string `json:"channel"`
	Duration    string `json:"duration"`
	Views       uint64 `json:"views"`
	Likes       uint64 `json:"likes"`
	Description string `json:"description,omitempty"`
}

func playlistURL(id string) string {
	return "https://www.youtube.com/playlist?list=" + id
}

// maxPageSize is the largest maxResults the Data API accepts on a list call.
// Anything above it is rejected outright, so requests are clamped rather than
// forwarded into a 400.
const maxPageSize = 50

// pageSize resolves a caller-supplied maxResults: unset (zero or negative)
// falls back to fallback, and oversized values are clamped to the API's limit.
func pageSize(requested, fallback int64) int64 {
	if requested <= 0 {
		return fallback
	}
	return min(requested, maxPageSize)
}
