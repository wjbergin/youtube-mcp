package yt

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
)

// defaultSearchResults is deliberately below the API's page limit: a search
// costs 100 quota units, so callers must opt in to a wider sweep.
const defaultSearchResults = 10

// SearchVideos searches only YouTube videos. Oversized requests are clamped to
// the API's page limit; unset values use the quota-conscious default.
func (c *Client) SearchVideos(ctx context.Context, query string, maxResults int64) ([]SearchResult, error) {
	resp, err := c.svc.Search.List([]string{"snippet"}).
		Q(query).
		Type("video").
		MaxResults(pageSize(maxResults, defaultSearchResults)).
		Context(ctx).
		Do()
	if err != nil {
		return nil, friendlyError(err)
	}

	results := make([]SearchResult, 0, len(resp.Items))
	for _, item := range resp.Items {
		result := SearchResult{}
		if item.Id != nil {
			result.VideoID = item.Id.VideoId
		}
		if item.Snippet != nil {
			result.Title = item.Snippet.Title
			result.Channel = item.Snippet.ChannelTitle
			result.PublishedAt = item.Snippet.PublishedAt
		}
		results = append(results, result)
	}
	return results, nil
}

// GetVideo returns the details of one video, including a human-readable
// duration derived from the API's ISO 8601 value.
func (c *Client) GetVideo(ctx context.Context, videoID string) (Video, error) {
	resp, err := c.svc.Videos.List([]string{"snippet", "contentDetails", "statistics"}).
		Id(videoID).
		Context(ctx).
		Do()
	if err != nil {
		return Video{}, friendlyError(err)
	}
	if len(resp.Items) == 0 {
		return Video{}, fmt.Errorf("not found: no video with id %q", videoID)
	}

	item := resp.Items[0]
	video := Video{ID: item.Id}
	if item.Snippet != nil {
		video.Title = item.Snippet.Title
		video.Channel = item.Snippet.ChannelTitle
		video.Description = item.Snippet.Description
	}
	if item.ContentDetails != nil {
		video.Duration = humanDuration(item.ContentDetails.Duration)
	}
	if item.Statistics != nil {
		video.Views = item.Statistics.ViewCount
		video.Likes = item.Statistics.LikeCount
	}
	return video, nil
}

var isoDuration = regexp.MustCompile(`^PT(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?$`)

// humanDuration converts an hours/minutes/seconds ISO 8601 duration such as
// PT1H2M3S into 1:02:03. Other ISO duration forms pass through unchanged.
func humanDuration(iso string) string {
	match := isoDuration.FindStringSubmatch(iso)
	if match == nil {
		return iso
	}
	hours, _ := strconv.Atoi(zeroIfEmpty(match[1]))
	minutes, _ := strconv.Atoi(zeroIfEmpty(match[2]))
	seconds, _ := strconv.Atoi(zeroIfEmpty(match[3]))
	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%d:%02d", minutes, seconds)
}

func zeroIfEmpty(value string) string {
	if value == "" {
		return "0"
	}
	return value
}
