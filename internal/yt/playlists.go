package yt

import (
	"context"
	"fmt"

	ytapi "google.golang.org/api/youtube/v3"
)

func (c *Client) ListPlaylists(ctx context.Context, maxResults int64) ([]Playlist, error) {
	if maxResults <= 0 {
		maxResults = 50
	}
	resp, err := c.svc.Playlists.List([]string{"snippet", "status", "contentDetails"}).
		Mine(true).MaxResults(maxResults).Context(ctx).Do()
	if err != nil {
		return nil, friendlyError(err)
	}
	out := make([]Playlist, 0, len(resp.Items))
	for _, p := range resp.Items {
		out = append(out, fromAPIPlaylist(p))
	}
	return out, nil
}

func (c *Client) CreatePlaylist(ctx context.Context, title, description, privacy string) (Playlist, error) {
	if privacy == "" {
		privacy = "private"
	}
	p := &ytapi.Playlist{
		Snippet: &ytapi.PlaylistSnippet{Title: title, Description: description},
		Status:  &ytapi.PlaylistStatus{PrivacyStatus: privacy},
	}
	created, err := c.svc.Playlists.Insert([]string{"snippet", "status"}, p).Context(ctx).Do()
	if err != nil {
		return Playlist{}, friendlyError(err)
	}
	return fromAPIPlaylist(created), nil
}

// UpdatePlaylist merges the provided fields into the playlist's current
// state: the API replaces the whole snippet on update, so unchanged fields
// must be re-sent with their existing values. nil means "leave unchanged".
func (c *Client) UpdatePlaylist(ctx context.Context, id string, title, description, privacy *string) (Playlist, error) {
	resp, err := c.svc.Playlists.List([]string{"snippet", "status"}).Id(id).Context(ctx).Do()
	if err != nil {
		return Playlist{}, friendlyError(err)
	}
	if len(resp.Items) == 0 {
		return Playlist{}, fmt.Errorf("not found: no playlist with id %q", id)
	}
	cur := resp.Items[0]
	if title != nil {
		cur.Snippet.Title = *title
	}
	if description != nil {
		cur.Snippet.Description = *description
	}
	if privacy != nil {
		if cur.Status == nil {
			cur.Status = &ytapi.PlaylistStatus{}
		}
		cur.Status.PrivacyStatus = *privacy
	}
	updated, err := c.svc.Playlists.Update([]string{"snippet", "status"}, cur).Context(ctx).Do()
	if err != nil {
		return Playlist{}, friendlyError(err)
	}
	return fromAPIPlaylist(updated), nil
}

func (c *Client) DeletePlaylist(ctx context.Context, id string) error {
	return friendlyError(c.svc.Playlists.Delete(id).Context(ctx).Do())
}

func fromAPIPlaylist(p *ytapi.Playlist) Playlist {
	out := Playlist{ID: p.Id, URL: playlistURL(p.Id)}
	if p.Snippet != nil {
		out.Title = p.Snippet.Title
		out.Description = p.Snippet.Description
	}
	if p.Status != nil {
		out.Privacy = p.Status.PrivacyStatus
	}
	if p.ContentDetails != nil {
		out.ItemCount = p.ContentDetails.ItemCount
	}
	return out
}
