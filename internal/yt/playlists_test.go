package yt

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/api/option"
)

// testClient returns a Client whose HTTP calls hit mux instead of Google.
func testClient(t *testing.T, mux *http.ServeMux) *Client {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := New(context.Background(), srv.Client(), option.WithEndpoint(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestListPlaylists(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/youtube/v3/playlists", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("mine") != "true" {
			t.Errorf("mine=true not set: %s", r.URL)
		}
		fmt.Fprint(w, `{"items": [{
			"id": "PL1",
			"snippet": {"title": "Guitar", "description": "lessons"},
			"status": {"privacyStatus": "private"},
			"contentDetails": {"itemCount": 3}
		}]}`)
	})
	got, err := testClient(t, mux).ListPlaylists(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	want := Playlist{ID: "PL1", Title: "Guitar", Description: "lessons",
		Privacy: "private", ItemCount: 3, URL: "https://www.youtube.com/playlist?list=PL1"}
	if len(got) != 1 || got[0] != want {
		t.Errorf("got %+v, want [%+v]", got, want)
	}
}

func TestCreatePlaylistDefaultsPrivate(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/youtube/v3/playlists", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Snippet struct {
				Title string `json:"title"`
			} `json:"snippet"`
			Status struct {
				PrivacyStatus string `json:"privacyStatus"`
			} `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Status.PrivacyStatus != "private" {
			t.Errorf("privacy %q, want private by default", body.Status.PrivacyStatus)
		}
		fmt.Fprintf(w, `{"id": "PLnew", "snippet": {"title": %q}, "status": {"privacyStatus": "private"}}`, body.Snippet.Title)
	})
	got, err := testClient(t, mux).CreatePlaylist(context.Background(), "New List", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "PLnew" || got.Title != "New List" {
		t.Errorf("got %+v", got)
	}
}

func TestUpdatePlaylistMergesSnippet(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/youtube/v3/playlists", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			fmt.Fprint(w, `{"items": [{
				"id": "PL1",
				"snippet": {"title": "Old Title", "description": "old desc"},
				"status": {"privacyStatus": "private"}
			}]}`)
		case http.MethodPut:
			var body struct {
				Snippet struct {
					Title       string `json:"title"`
					Description string `json:"description"`
				} `json:"snippet"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			// Only the title was changed; the description must be preserved.
			if body.Snippet.Title != "New Title" || body.Snippet.Description != "old desc" {
				t.Errorf("merge failed, sent snippet: %+v", body.Snippet)
			}
			fmt.Fprint(w, `{"id": "PL1", "snippet": {"title": "New Title", "description": "old desc"}, "status": {"privacyStatus": "private"}}`)
		}
	})
	title := "New Title"
	got, err := testClient(t, mux).UpdatePlaylist(context.Background(), "PL1", &title, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "New Title" || got.Description != "old desc" {
		t.Errorf("got %+v", got)
	}
}

func TestUpdatePlaylistNotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/youtube/v3/playlists", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"items": []}`)
	})
	title := "x"
	_, err := testClient(t, mux).UpdatePlaylist(context.Background(), "PLmissing", &title, nil, nil)
	if err == nil {
		t.Fatal("expected not-found error")
	}
}

func TestDeletePlaylist(t *testing.T) {
	var deleted string
	mux := http.NewServeMux()
	mux.HandleFunc("/youtube/v3/playlists", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleted = r.URL.Query().Get("id")
			w.WriteHeader(http.StatusNoContent)
		}
	})
	if err := testClient(t, mux).DeletePlaylist(context.Background(), "PL1"); err != nil {
		t.Fatal(err)
	}
	if deleted != "PL1" {
		t.Errorf("deleted %q, want PL1", deleted)
	}
}
