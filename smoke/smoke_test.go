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

// The Data API is not read-after-write consistent for a newly created
// playlist. For the first seconds after Playlists.insert, a PlaylistItems
// insert can fail with 409 ("The operation was aborted"), and a read-back can
// 404 the playlist outright or report it as still empty. Observed settling
// times are ~1s for the insert and ~5s for the read-back, so the window below
// is generous enough to absorb a bad day without hanging the suite.
const (
	propagationWindow = 60 * time.Second
	pollInterval      = 3 * time.Second
)

func TestLivePlaylistLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
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

	// Only the transient conflict is retried here. Any other insert failure is
	// a real one, and retrying a write that may have landed would duplicate the
	// entry and break the assertions below.
	poll(ctx, t, "add video to freshly created playlist", isAbortedConflict, func() error {
		_, err := client.AddVideo(ctx, playlist.ID, videoID, nil)
		return err
	})

	// Reads are idempotent, so the read-back retries on anything: a 404 for the
	// not-yet-propagated playlist, and an empty or partial page alike.
	poll(ctx, t, "read back playlist contents", anyError, func() error {
		items, _, err := client.ListPlaylistItems(ctx, playlist.ID, 10, "")
		if err != nil {
			return err
		}
		if len(items) != 1 || items[0].VideoID != videoID {
			return fmt.Errorf("playlist contents wrong: %+v", items)
		}
		return nil
	})

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

// poll runs op until it succeeds or propagationWindow elapses, failing the test
// with the last error either way. Errors that retryable rejects fail
// immediately, so a genuine break surfaces as itself instead of as a timeout.
func poll(ctx context.Context, t *testing.T, desc string, retryable func(error) bool, op func() error) {
	t.Helper()

	deadline := time.Now().Add(propagationWindow)
	for attempt := 1; ; attempt++ {
		err := op()
		switch {
		case err == nil:
			return
		case !retryable(err):
			t.Fatalf("%s: %v", desc, err)
		case !time.Now().Before(deadline):
			t.Fatalf("%s: still failing after %v and %d attempts: %v",
				desc, propagationWindow, attempt, err)
		}
		t.Logf("%s: attempt %d not ready (%v); retrying in %v", desc, attempt, err, pollInterval)

		select {
		case <-ctx.Done():
			t.Fatalf("%s: %v (last error: %v)", desc, ctx.Err(), err)
		case <-time.After(pollInterval):
		}
	}
}

// isAbortedConflict reports whether err is the transient 409 the Data API
// returns while a freshly created playlist is still propagating. friendlyError
// renders *googleapi.Error as text rather than wrapping it, so the status is
// matched on the message.
func isAbortedConflict(err error) bool {
	return err != nil && strings.Contains(err.Error(), "YouTube API error 409")
}

func anyError(err error) bool { return err != nil }
