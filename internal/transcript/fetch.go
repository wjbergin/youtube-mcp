package transcript

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const defaultPlayerURL = "https://www.youtube.com/youtubei/v1/player"

// Fetcher retrieves transcripts via YouTube's InnerTube player endpoint.
// The endpoint is unofficial and unauthenticated; all knowledge of it is
// confined to this package so a breakage is a one-package fix.
type Fetcher struct {
	HTTP      *http.Client
	PlayerURL string
}

func New() *Fetcher {
	return &Fetcher{HTTP: http.DefaultClient, PlayerURL: defaultPlayerURL}
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

func (f *Fetcher) Fetch(ctx context.Context, videoID, lang string, withTimestamps bool) (string, error) {
	tracks, err := f.fetchTracks(ctx, videoID)
	if err != nil {
		return "", err
	}
	track, err := selectTrack(tracks, lang)
	if err != nil {
		return "", err
	}
	sep := "?"
	if strings.Contains(track.BaseURL, "?") {
		sep = "&"
	}
	data, err := f.get(ctx, track.BaseURL+sep+"fmt=json3")
	if err != nil {
		return "", fmt.Errorf("fetching caption track: %w", err)
	}
	return parseJSON3(data, withTimestamps)
}

func (f *Fetcher) fetchTracks(ctx context.Context, videoID string) ([]captionTrack, error) {
	body, err := json.Marshal(map[string]any{
		"context": map[string]any{
			"client": map[string]any{
				"clientName":    "WEB",
				"clientVersion": "2.20250101.00.00",
			},
		},
		"videoId": videoID,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.PlayerURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := f.HTTP.Do(req)
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
	resp, err := f.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("caption URL returned %s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}
