// Package transcript retrieves YouTube video transcripts via the unofficial
// InnerTube endpoint.
package transcript

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const (
	defaultPlayerURL = "https://www.youtube.com/youtubei/v1/player"
	defaultTimeout   = 30 * time.Second
)

// Fetcher retrieves transcripts via YouTube's InnerTube player endpoint.
// The endpoint is unofficial and unauthenticated; all knowledge of it is
// confined to this package so a breakage is a one-package fix.
//
// Requests identify as the ANDROID client: WEB-client player calls are
// PO-token-gated and report even plainly available videos as unavailable.
//
// The zero value is usable; HTTP and PlayerURL fall back to defaults.
type Fetcher struct {
	HTTP      *http.Client
	PlayerURL string
}

// New returns a Fetcher pointing at the live InnerTube endpoint, with a
// request timeout so a stalled connection cannot hang a tool call.
func New() *Fetcher {
	return &Fetcher{HTTP: &http.Client{Timeout: defaultTimeout}, PlayerURL: defaultPlayerURL}
}

func (f *Fetcher) client() *http.Client {
	if f.HTTP == nil {
		return http.DefaultClient
	}
	return f.HTTP
}

func (f *Fetcher) playerURL() string {
	if f.PlayerURL == "" {
		return defaultPlayerURL
	}
	return f.PlayerURL
}

type playerResponse struct {
	PlayabilityStatus struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	} `json:"playabilityStatus"`
	Captions struct {
		Renderer struct {
			CaptionTracks []captionTrack `json:"captionTracks"`
		} `json:"playerCaptionsTracklistRenderer"`
	} `json:"captions"`
}

// Fetch returns the transcript of videoID as plain text, one line per caption
// event.
//
// lang is a BCP-47 tag such as "en" or "pt-BR". An exact tag match wins; if
// none exists, matching falls back to the base language, so "en" accepts a
// track coded "en-GB". Human-made captions are preferred over auto-generated
// ones. With withTimestamps, each line is prefixed [m:ss] (or [h:mm:ss] past
// an hour).
//
// Three errors are actionable by the caller: a video with no captions at all,
// a video that is unavailable or restricted (the message carries YouTube's
// reason), and a request for a language the video lacks (the message lists the
// available tracks, marking auto-generated ones).
func (f *Fetcher) Fetch(ctx context.Context, videoID, lang string, withTimestamps bool) (string, error) {
	tracks, err := f.fetchTracks(ctx, videoID)
	if err != nil {
		return "", err
	}
	track, err := selectTrack(tracks, lang)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(track.BaseURL)
	if err != nil {
		return "", fmt.Errorf("caption track URL: %w", err)
	}
	q := u.Query()
	q.Set("fmt", "json3") // replaces any existing fmt (ANDROID URLs carry fmt=srv3)
	u.RawQuery = q.Encode()
	data, err := f.get(ctx, u.String())
	if err != nil {
		return "", fmt.Errorf("fetching caption track: %w", err)
	}
	return parseJSON3(data, withTimestamps)
}

func (f *Fetcher) fetchTracks(ctx context.Context, videoID string) ([]captionTrack, error) {
	body, err := json.Marshal(map[string]any{
		"context": map[string]any{
			"client": map[string]any{
				"clientName":        "ANDROID",
				"clientVersion":     "20.10.38",
				"androidSdkVersion": 30,
				"hl":                "en",
				"gl":                "US",
			},
		},
		"videoId": videoID,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.playerURL(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := f.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling player endpoint: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("player endpoint returned %s", resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var pr playerResponse
	if err := json.Unmarshal(data, &pr); err != nil {
		return nil, fmt.Errorf("parsing player response: %w", err)
	}
	if s := pr.PlayabilityStatus.Status; s != "" && s != "OK" {
		reason := pr.PlayabilityStatus.Reason
		if reason == "" {
			reason = s
		}
		return nil, fmt.Errorf("video unavailable or restricted: %s", reason)
	}
	if len(pr.Captions.Renderer.CaptionTracks) == 0 {
		return nil, fmt.Errorf("video has no captions")
	}
	return pr.Captions.Renderer.CaptionTracks, nil
}

func (f *Fetcher) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := f.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("caption URL returned %s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}
