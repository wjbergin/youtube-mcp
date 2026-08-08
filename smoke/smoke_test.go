//go:build smoke

// Package smoke exercises the live YouTube API end to end. Run manually:
//
//	go test -tags smoke ./smoke -v
//
// It requires ~/.config/youtube-mcp/{credentials,token}.json and creates then
// deletes a throwaway private playlist in the authenticated account.
package smoke

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"youtube-mcp/internal/auth"
	"youtube-mcp/internal/transcript"
	"youtube-mcp/internal/yt"
)

func TestLivePlaylistLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	httpClient, err := auth.HTTPClient(ctx)
	if err != nil {
		t.Skipf("not authenticated: %v", err)
	}
	client, err := yt.New(ctx, httpClient)
	if err != nil {
		t.Fatal(err)
	}

	name := fmt.Sprintf("youtube-mcp-smoke-%d", time.Now().Unix())
	playlist, err := client.CreatePlaylist(ctx, name, "temporary youtube-mcp smoke-test playlist", "private")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if err := client.DeletePlaylist(cleanupCtx, playlist.ID); err != nil {
			t.Errorf("cleanup failed; delete playlist %s manually: %v", playlist.ID, err)
		}
	}()

	// "Me at the zoo", YouTube's first video.
	const videoID = "jNQXAC9IVRw"
	if _, err := client.AddVideo(ctx, playlist.ID, videoID, nil); err != nil {
		t.Fatal(err)
	}
	items, _, err := client.ListPlaylistItems(ctx, playlist.ID, 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].VideoID != videoID {
		t.Fatalf("playlist contents wrong: %+v", items)
	}
	removed, err := client.RemoveVideo(ctx, playlist.ID, videoID)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Errorf("removed %d entries, want 1", removed)
	}
}

func TestLiveTranscript(t *testing.T) {
	text, err := transcript.New().Fetch(context.Background(), "jNQXAC9IVRw", "en", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(text), "elephant") {
		t.Errorf("transcript does not mention elephants; got %q", text)
	}
}
