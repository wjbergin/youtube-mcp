// Package tools defines the MCP tool surface. Handlers depend on narrow
// interfaces so they can be tested with fakes and so the server can start
// before the user has authenticated.
package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"youtube-mcp/internal/yt"
)

// Service is the authenticated YouTube API surface used by MCP handlers.
// *yt.Client implements it.
type Service interface {
	ListPlaylists(context.Context, int64) ([]yt.Playlist, error)
	CreatePlaylist(context.Context, string, string, string) (yt.Playlist, error)
	UpdatePlaylist(context.Context, string, *string, *string, *string) (yt.Playlist, error)
	DeletePlaylist(context.Context, string) error
	ListPlaylistItems(context.Context, string, int64, string) ([]yt.PlaylistItem, string, error)
	AddVideo(context.Context, string, string, *int64) (yt.PlaylistItem, error)
	RemoveVideo(context.Context, string, string) (int, error)
	SearchVideos(context.Context, string, int64) ([]yt.SearchResult, error)
	GetVideo(context.Context, string) (yt.Video, error)
}

// Provider obtains an authenticated service lazily. This lets the MCP server
// start before OAuth setup and surface an actionable error from individual
// tools instead of failing the whole process.
type Provider func(context.Context) (Service, error)

// TranscriptFetcher is the unauthenticated transcript surface.
type TranscriptFetcher interface {
	Fetch(context.Context, string, string, bool) (string, error)
}

type ListPlaylistsInput struct {
	MaxResults int64 `json:"max_results,omitempty" jsonschema:"maximum number of playlists to return (default 50)"`
}

type ListPlaylistsOutput struct {
	Playlists []yt.Playlist `json:"playlists"`
}

type CreatePlaylistInput struct {
	Title         string `json:"title" jsonschema:"title of the new playlist"`
	Description   string `json:"description,omitempty" jsonschema:"playlist description"`
	PrivacyStatus string `json:"privacy_status,omitempty" jsonschema:"private (default), unlisted, or public"`
}

type PlaylistOutput struct {
	Playlist yt.Playlist `json:"playlist"`
}

type UpdatePlaylistInput struct {
	PlaylistID    string  `json:"playlist_id" jsonschema:"id of the playlist to update"`
	Title         *string `json:"title,omitempty" jsonschema:"new title (omit to keep current)"`
	Description   *string `json:"description,omitempty" jsonschema:"new description (omit to keep current)"`
	PrivacyStatus *string `json:"privacy_status,omitempty" jsonschema:"new privacy: private, unlisted, or public (omit to keep current)"`
}

type DeletePlaylistInput struct {
	PlaylistID string `json:"playlist_id" jsonschema:"id of the playlist to permanently delete"`
}

type DeletePlaylistOutput struct {
	Deleted string `json:"deleted"`
}

type ListItemsInput struct {
	PlaylistID string `json:"playlist_id" jsonschema:"playlist to list"`
	MaxResults int64  `json:"max_results,omitempty" jsonschema:"maximum items to return (default 50)"`
	PageToken  string `json:"page_token,omitempty" jsonschema:"token from a previous call to fetch the next page"`
}

type ListItemsOutput struct {
	Items         []yt.PlaylistItem `json:"items"`
	NextPageToken string            `json:"next_page_token,omitempty"`
}

type AddVideoInput struct {
	PlaylistID string `json:"playlist_id" jsonschema:"playlist to add to"`
	VideoID    string `json:"video_id" jsonschema:"video to add"`
	Position   *int64 `json:"position,omitempty" jsonschema:"zero-based position in the playlist (omit to append)"`
}

type ItemOutput struct {
	Item yt.PlaylistItem `json:"item"`
}

type RemoveVideoInput struct {
	PlaylistID string `json:"playlist_id" jsonschema:"playlist to remove from"`
	VideoID    string `json:"video_id" jsonschema:"video to remove (removes every occurrence)"`
}

type RemoveVideoOutput struct {
	Removed int `json:"removed"`
}

type SearchInput struct {
	Query      string `json:"query" jsonschema:"search terms"`
	MaxResults int64  `json:"max_results,omitempty" jsonschema:"maximum results (default 10, max 50)"`
}

type SearchOutput struct {
	Results []yt.SearchResult `json:"results"`
}

type GetVideoInput struct {
	VideoID string `json:"video_id" jsonschema:"video id"`
}

type GetVideoOutput struct {
	Video yt.Video `json:"video"`
}

type TranscriptInput struct {
	VideoID        string `json:"video_id" jsonschema:"video id"`
	Language       string `json:"language,omitempty" jsonschema:"language code such as en or pt-BR (default en)"`
	WithTimestamps bool   `json:"with_timestamps,omitempty" jsonschema:"prefix each line with an [m:ss] or [h:mm:ss] timestamp"`
}

type TranscriptOutput struct {
	Transcript string `json:"transcript"`
}

// Register adds the complete ten-tool YouTube surface to server.
func Register(server *mcp.Server, provider Provider, transcripts TranscriptFetcher) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_playlists",
		Description: "List the authenticated user's YouTube playlists.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListPlaylistsInput) (*mcp.CallToolResult, ListPlaylistsOutput, error) {
		service, err := provider(ctx)
		if err != nil {
			return nil, ListPlaylistsOutput{}, err
		}
		playlists, err := service.ListPlaylists(ctx, input.MaxResults)
		if err != nil {
			return nil, ListPlaylistsOutput{}, err
		}
		return nil, ListPlaylistsOutput{Playlists: playlists}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_playlist",
		Description: "Create a new YouTube playlist (private unless specified otherwise).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CreatePlaylistInput) (*mcp.CallToolResult, PlaylistOutput, error) {
		service, err := provider(ctx)
		if err != nil {
			return nil, PlaylistOutput{}, err
		}
		playlist, err := service.CreatePlaylist(ctx, input.Title, input.Description, input.PrivacyStatus)
		if err != nil {
			return nil, PlaylistOutput{}, err
		}
		return nil, PlaylistOutput{Playlist: playlist}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_playlist",
		Description: "Update a playlist's title, description, or privacy. Omitted fields keep their current values.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input UpdatePlaylistInput) (*mcp.CallToolResult, PlaylistOutput, error) {
		service, err := provider(ctx)
		if err != nil {
			return nil, PlaylistOutput{}, err
		}
		playlist, err := service.UpdatePlaylist(ctx, input.PlaylistID, input.Title, input.Description, input.PrivacyStatus)
		if err != nil {
			return nil, PlaylistOutput{}, err
		}
		return nil, PlaylistOutput{Playlist: playlist}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_playlist",
		Description: "PERMANENTLY delete a playlist and all its entries. This cannot be undone.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input DeletePlaylistInput) (*mcp.CallToolResult, DeletePlaylistOutput, error) {
		service, err := provider(ctx)
		if err != nil {
			return nil, DeletePlaylistOutput{}, err
		}
		if err := service.DeletePlaylist(ctx, input.PlaylistID); err != nil {
			return nil, DeletePlaylistOutput{}, err
		}
		return nil, DeletePlaylistOutput{Deleted: input.PlaylistID}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_playlist_items",
		Description: "List one page of videos in a playlist, including playlist-item IDs and the next-page token.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ListItemsInput) (*mcp.CallToolResult, ListItemsOutput, error) {
		service, err := provider(ctx)
		if err != nil {
			return nil, ListItemsOutput{}, err
		}
		items, next, err := service.ListPlaylistItems(ctx, input.PlaylistID, input.MaxResults, input.PageToken)
		if err != nil {
			return nil, ListItemsOutput{}, err
		}
		return nil, ListItemsOutput{Items: items, NextPageToken: next}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "add_video_to_playlist",
		Description: "Add a video to a playlist, appending unless a position is given.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input AddVideoInput) (*mcp.CallToolResult, ItemOutput, error) {
		service, err := provider(ctx)
		if err != nil {
			return nil, ItemOutput{}, err
		}
		item, err := service.AddVideo(ctx, input.PlaylistID, input.VideoID, input.Position)
		if err != nil {
			return nil, ItemOutput{}, err
		}
		return nil, ItemOutput{Item: item}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "remove_video_from_playlist",
		Description: "Remove every occurrence of a video from a playlist and report how many entries were removed.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input RemoveVideoInput) (*mcp.CallToolResult, RemoveVideoOutput, error) {
		service, err := provider(ctx)
		if err != nil {
			return nil, RemoveVideoOutput{}, err
		}
		removed, err := service.RemoveVideo(ctx, input.PlaylistID, input.VideoID)
		if err != nil {
			return nil, RemoveVideoOutput{}, err
		}
		return nil, RemoveVideoOutput{Removed: removed}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_videos",
		Description: "Search YouTube for videos. Costs 100 API quota units per call (daily free quota is 10,000).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input SearchInput) (*mcp.CallToolResult, SearchOutput, error) {
		service, err := provider(ctx)
		if err != nil {
			return nil, SearchOutput{}, err
		}
		results, err := service.SearchVideos(ctx, input.Query, input.MaxResults)
		if err != nil {
			return nil, SearchOutput{}, err
		}
		return nil, SearchOutput{Results: results}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_video",
		Description: "Get a video's title, channel, duration, view and like counts, and description.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input GetVideoInput) (*mcp.CallToolResult, GetVideoOutput, error) {
		service, err := provider(ctx)
		if err != nil {
			return nil, GetVideoOutput{}, err
		}
		video, err := service.GetVideo(ctx, input.VideoID)
		if err != nil {
			return nil, GetVideoOutput{}, err
		}
		return nil, GetVideoOutput{Video: video}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_transcript",
		Description: "Fetch the transcript of a public YouTube video without authentication.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input TranscriptInput) (*mcp.CallToolResult, TranscriptOutput, error) {
		language := input.Language
		if language == "" {
			language = "en"
		}
		text, err := transcripts.Fetch(ctx, input.VideoID, language, input.WithTimestamps)
		if err != nil {
			return nil, TranscriptOutput{}, err
		}
		return nil, TranscriptOutput{Transcript: text}, nil
	})
}
