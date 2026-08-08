package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"youtube-mcp/internal/yt"
)

type fakeService struct {
	playlists []yt.Playlist
	removed   int
}

func (fake *fakeService) ListPlaylists(context.Context, int64) ([]yt.Playlist, error) {
	return fake.playlists, nil
}

func (*fakeService) CreatePlaylist(_ context.Context, title, _, privacy string) (yt.Playlist, error) {
	if privacy == "" {
		privacy = "private"
	}
	return yt.Playlist{ID: "PLnew", Title: title, Privacy: privacy}, nil
}

func (*fakeService) UpdatePlaylist(_ context.Context, id string, title, _, _ *string) (yt.Playlist, error) {
	result := yt.Playlist{ID: id, Title: "Updated"}
	if title != nil {
		result.Title = *title
	}
	return result, nil
}

func (*fakeService) DeletePlaylist(context.Context, string) error {
	return nil
}

func (*fakeService) ListPlaylistItems(context.Context, string, int64, string) ([]yt.PlaylistItem, string, error) {
	return []yt.PlaylistItem{{ID: "i1", VideoID: "v1", Title: "T"}}, "next", nil
}

func (*fakeService) AddVideo(_ context.Context, _, videoID string, _ *int64) (yt.PlaylistItem, error) {
	return yt.PlaylistItem{ID: "i2", VideoID: videoID}, nil
}

func (fake *fakeService) RemoveVideo(context.Context, string, string) (int, error) {
	return fake.removed, nil
}

func (*fakeService) SearchVideos(context.Context, string, int64) ([]yt.SearchResult, error) {
	return []yt.SearchResult{{VideoID: "v1", Title: "Hit"}}, nil
}

func (*fakeService) GetVideo(_ context.Context, videoID string) (yt.Video, error) {
	return yt.Video{ID: videoID, Title: "A Video"}, nil
}

type fakeTranscripts struct{}

func (fakeTranscripts) Fetch(_ context.Context, _ string, language string, withTimestamps bool) (string, error) {
	text := "hello transcript in " + language
	if withTimestamps {
		text = "[0:00] " + text
	}
	return text, nil
}

func session(t *testing.T, provider Provider) *mcp.ClientSession {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "youtube-mcp-test", Version: "0.0.0"}, nil)
	Register(server, provider, fakeTranscripts{})
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { serverSession.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { clientSession.Close() })
	return clientSession
}

func okProvider(service Service) Provider {
	return func(context.Context) (Service, error) { return service, nil }
}

func callTool(t *testing.T, client *mcp.ClientSession, name string, arguments map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := client.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func contentText(result *mcp.CallToolResult) string {
	var text strings.Builder
	for _, content := range result.Content {
		if block, ok := content.(*mcp.TextContent); ok {
			text.WriteString(block.Text)
		}
	}
	return text.String()
}

func TestAllTenToolsRegistered(t *testing.T) {
	client := session(t, okProvider(&fakeService{}))
	response, err := client.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"add_video_to_playlist", "create_playlist", "delete_playlist",
		"get_transcript", "get_video", "list_playlist_items",
		"list_playlists", "remove_video_from_playlist", "search_videos",
		"update_playlist",
	}
	got := make([]string, 0, len(response.Tools))
	for _, tool := range response.Tools {
		got = append(got, tool.Name)
	}
	sort.Strings(got)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("registered tools = %v, want %v", got, want)
	}
}

func TestListPlaylistsTool(t *testing.T) {
	service := &fakeService{playlists: []yt.Playlist{{ID: "PL1", Title: "Guitar"}}}
	result := callTool(t, session(t, okProvider(service)), "list_playlists", map[string]any{})
	if result.IsError {
		t.Fatalf("tool errored: %v", result.Content)
	}
	text := fmt.Sprintf("%v", result.StructuredContent)
	if !strings.Contains(text, "PL1") || !strings.Contains(text, "Guitar") {
		t.Errorf("structured content missing playlist: %v", result.StructuredContent)
	}
}

func TestMutationAndReadToolsReturnStructuredContent(t *testing.T) {
	client := session(t, okProvider(&fakeService{removed: 2}))
	tests := []struct {
		name      string
		arguments map[string]any
		contains  string
	}{
		{name: "create_playlist", arguments: map[string]any{"title": "New"}, contains: "PLnew"},
		{name: "update_playlist", arguments: map[string]any{"playlist_id": "PL1", "title": "Changed"}, contains: "Changed"},
		{name: "delete_playlist", arguments: map[string]any{"playlist_id": "PL1"}, contains: "PL1"},
		{name: "list_playlist_items", arguments: map[string]any{"playlist_id": "PL1"}, contains: "next"},
		{name: "add_video_to_playlist", arguments: map[string]any{"playlist_id": "PL1", "video_id": "v2", "position": 0}, contains: "v2"},
		{name: "remove_video_from_playlist", arguments: map[string]any{"playlist_id": "PL1", "video_id": "v1"}, contains: "2"},
		{name: "search_videos", arguments: map[string]any{"query": "go"}, contains: "Hit"},
		{name: "get_video", arguments: map[string]any{"video_id": "v1"}, contains: "A Video"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := callTool(t, client, test.name, test.arguments)
			if result.IsError {
				t.Fatalf("tool errored: %v", result.Content)
			}
			if !strings.Contains(fmt.Sprintf("%v", result.StructuredContent), test.contains) {
				t.Errorf("structured content %v missing %q", result.StructuredContent, test.contains)
			}
		})
	}
}

func TestGetTranscriptToolDefaultsEnglishWithoutProvider(t *testing.T) {
	provider := func(context.Context) (Service, error) {
		return nil, fmt.Errorf("provider must not be called for transcripts")
	}
	result := callTool(t, session(t, provider), "get_transcript", map[string]any{"video_id": "v1"})
	if result.IsError {
		t.Fatalf("tool errored: %v", result.Content)
	}
	if !strings.Contains(fmt.Sprintf("%v", result.StructuredContent), "hello transcript in en") {
		t.Errorf("transcript missing or wrong default language: %v", result.StructuredContent)
	}
}

func TestGetTranscriptToolForwardsOptions(t *testing.T) {
	result := callTool(t, session(t, okProvider(&fakeService{})), "get_transcript", map[string]any{
		"video_id": "v1", "language": "de", "with_timestamps": true,
	})
	text := fmt.Sprintf("%v", result.StructuredContent)
	if !strings.Contains(text, "[0:00]") || !strings.Contains(text, "in de") {
		t.Errorf("transcript options not forwarded: %v", result.StructuredContent)
	}
}

func TestUnauthenticatedProviderReturnsToolError(t *testing.T) {
	failing := func(context.Context) (Service, error) {
		return nil, fmt.Errorf("not authenticated: run `youtube-mcp auth` to sign in")
	}
	result := callTool(t, session(t, failing), "list_playlists", map[string]any{})
	if !result.IsError {
		t.Fatal("expected tool error when unauthenticated")
	}
	if !strings.Contains(contentText(result), "youtube-mcp auth") {
		t.Errorf("error content not instructive: %v", result.Content)
	}
}

func TestMissingRequiredInputIsToolError(t *testing.T) {
	result := callTool(t, session(t, okProvider(&fakeService{})), "get_video", map[string]any{})
	if !result.IsError {
		t.Fatal("expected schema validation error for missing video_id")
	}
}
