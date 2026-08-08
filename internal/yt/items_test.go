package yt

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestListPlaylistItemsPaginates(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/youtube/v3/playlistItems", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("playlistId") != "PL1" {
			t.Errorf("playlistId not sent: %s", r.URL)
		}
		fmt.Fprint(w, `{"nextPageToken": "tok2", "items": [{
			"id": "item1",
			"snippet": {"title": "Video One", "position": 0, "resourceId": {"videoId": "vid1"}}
		}]}`)
	})
	items, next, err := testClient(t, mux).ListPlaylistItems(context.Background(), "PL1", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if next != "tok2" {
		t.Errorf("nextPageToken %q, want tok2", next)
	}
	want := PlaylistItem{ID: "item1", VideoID: "vid1", Title: "Video One", Position: 0}
	if len(items) != 1 || items[0] != want {
		t.Errorf("got %+v, want [%+v]", items, want)
	}
}

func TestAddVideoAtPositionZero(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/youtube/v3/playlistItems", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Snippet map[string]json.RawMessage `json:"snippet"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if _, ok := req.Snippet["position"]; !ok {
			t.Error("position 0 was omitted from the request body (ForceSendFields missing)")
		}
		fmt.Fprint(w, `{"id": "itemNew", "snippet": {"title": "V", "position": 0, "resourceId": {"videoId": "vid9"}}}`)
	})
	pos := int64(0)
	got, err := testClient(t, mux).AddVideo(context.Background(), "PL1", "vid9", &pos)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "itemNew" || got.VideoID != "vid9" {
		t.Errorf("got %+v", got)
	}
}

func TestRemoveVideoAllOccurrences(t *testing.T) {
	var deleted []string
	mux := http.NewServeMux()
	mux.HandleFunc("/youtube/v3/playlistItems", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if r.URL.Query().Get("pageToken") == "" {
				fmt.Fprint(w, `{"nextPageToken": "p2", "items": [
					{"id": "i1", "snippet": {"resourceId": {"videoId": "target"}}},
					{"id": "i2", "snippet": {"resourceId": {"videoId": "other"}}}
				]}`)
			} else {
				fmt.Fprint(w, `{"items": [
					{"id": "i3", "snippet": {"resourceId": {"videoId": "target"}}}
				]}`)
			}
		case http.MethodDelete:
			deleted = append(deleted, r.URL.Query().Get("id"))
			w.WriteHeader(http.StatusNoContent)
		}
	})
	n, err := testClient(t, mux).RemoveVideo(context.Background(), "PL1", "target")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("removed %d, want 2", n)
	}
	if len(deleted) != 2 || deleted[0] != "i1" || deleted[1] != "i3" {
		t.Errorf("deleted %v, want [i1 i3]", deleted)
	}
}

func TestListPlaylistItemsClampsMaxResults(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/youtube/v3/playlistItems", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("maxResults"); got != "50" {
			t.Errorf("maxResults %q, want capped value 50", got)
		}
		fmt.Fprint(w, `{"items": []}`)
	})
	if _, _, err := testClient(t, mux).ListPlaylistItems(context.Background(), "PL1", 100, ""); err != nil {
		t.Fatal(err)
	}
}

// A delete that fails part-way through has still changed the playlist, so the
// count and the message must report the entries that were actually removed.
func TestRemoveVideoReportsPartialProgress(t *testing.T) {
	deletes := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/youtube/v3/playlistItems", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			fmt.Fprint(w, `{"items": [
				{"id": "i1", "snippet": {"resourceId": {"videoId": "target"}}},
				{"id": "i2", "snippet": {"resourceId": {"videoId": "target"}}}
			]}`)
		case http.MethodDelete:
			deletes++
			if deletes == 2 {
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(w, `{"error": {"code": 500, "message": "backend error"}}`)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		}
	})
	n, err := testClient(t, mux).RemoveVideo(context.Background(), "PL1", "target")
	if err == nil {
		t.Fatal("expected the failed deletion to be reported")
	}
	if n != 1 {
		t.Errorf("removed count = %d, want the 1 entry that was actually deleted", n)
	}
	if !strings.Contains(err.Error(), "removed 1 of 2") {
		t.Errorf("error must state partial progress, got: %v", err)
	}
}

func TestRemoveVideoNotInPlaylist(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/youtube/v3/playlistItems", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"items": []}`)
	})
	_, err := testClient(t, mux).RemoveVideo(context.Background(), "PL1", "ghost")
	if err == nil || !strings.Contains(err.Error(), "not in playlist") {
		t.Errorf("want not-in-playlist error, got: %v", err)
	}
}
