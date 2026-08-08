package yt

import (
	"context"
	"fmt"

	ytapi "google.golang.org/api/youtube/v3"
)

// ListPlaylistItems returns one page of entries from playlistID together with
// the token for the next page, if one exists.
func (c *Client) ListPlaylistItems(ctx context.Context, playlistID string, maxResults int64, pageToken string) ([]PlaylistItem, string, error) {
	call := c.svc.PlaylistItems.List([]string{"snippet"}).
		PlaylistId(playlistID).
		MaxResults(pageSize(maxResults, maxPageSize)).
		Context(ctx)
	if pageToken != "" {
		call = call.PageToken(pageToken)
	}
	resp, err := call.Do()
	if err != nil {
		return nil, "", friendlyError(err)
	}

	items := make([]PlaylistItem, 0, len(resp.Items))
	for _, item := range resp.Items {
		items = append(items, fromAPIItem(item))
	}
	return items, resp.NextPageToken, nil
}

// AddVideo inserts videoID into playlistID. A nil position appends; an
// explicit zero is force-sent because the generated API type otherwise omits
// zero-valued fields.
func (c *Client) AddVideo(ctx context.Context, playlistID, videoID string, position *int64) (PlaylistItem, error) {
	snippet := &ytapi.PlaylistItemSnippet{
		PlaylistId: playlistID,
		ResourceId: &ytapi.ResourceId{Kind: "youtube#video", VideoId: videoID},
	}
	if position != nil {
		snippet.Position = *position
		snippet.ForceSendFields = append(snippet.ForceSendFields, "Position")
	}

	created, err := c.svc.PlaylistItems.Insert(
		[]string{"snippet"},
		&ytapi.PlaylistItem{Snippet: snippet},
	).Context(ctx).Do()
	if err != nil {
		return PlaylistItem{}, friendlyError(err)
	}
	return fromAPIItem(created), nil
}

// RemoveVideo deletes every occurrence of videoID from playlistID and returns
// the number removed. It scans every page before deleting so pagination is not
// disturbed by mutations while matches are being resolved.
//
// A deletion that fails part-way through has still changed the playlist, so the
// count and the error both report the entries already removed.
func (c *Client) RemoveVideo(ctx context.Context, playlistID, videoID string) (int, error) {
	var itemIDs []string
	pageToken := ""
	for {
		items, next, err := c.ListPlaylistItems(ctx, playlistID, 50, pageToken)
		if err != nil {
			return 0, err
		}
		for _, item := range items {
			if item.VideoID == videoID {
				itemIDs = append(itemIDs, item.ID)
			}
		}
		if next == "" {
			break
		}
		pageToken = next
	}

	if len(itemIDs) == 0 {
		return 0, fmt.Errorf("video %q is not in playlist %q", videoID, playlistID)
	}
	removed := 0
	for _, itemID := range itemIDs {
		if err := c.svc.PlaylistItems.Delete(itemID).Context(ctx).Do(); err != nil {
			return removed, fmt.Errorf("removed %d of %d entries before failing: %w",
				removed, len(itemIDs), friendlyError(err))
		}
		removed++
	}
	return removed, nil
}

func fromAPIItem(item *ytapi.PlaylistItem) PlaylistItem {
	out := PlaylistItem{ID: item.Id}
	if item.Snippet != nil {
		out.Title = item.Snippet.Title
		out.Position = item.Snippet.Position
		if item.Snippet.ResourceId != nil {
			out.VideoID = item.Snippet.ResourceId.VideoId
		}
	}
	return out
}
