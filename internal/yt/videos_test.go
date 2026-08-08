package yt

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestSearchVideos(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/youtube/v3/search", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("q") != "go tutorials" || query.Get("type") != "video" {
			t.Errorf("bad query: %s", r.URL)
		}
		if query.Get("maxResults") != "10" {
			t.Errorf("maxResults %q, want default 10", query.Get("maxResults"))
		}
		fmt.Fprint(w, `{"items": [{
			"id": {"videoId": "vid1"},
			"snippet": {"title": "Learn Go", "channelTitle": "GoChan", "publishedAt": "2025-01-01T00:00:00Z"}
		}]}`)
	})

	got, err := testClient(t, mux).SearchVideos(context.Background(), "go tutorials", 0)
	if err != nil {
		t.Fatal(err)
	}
	want := SearchResult{
		VideoID:     "vid1",
		Title:       "Learn Go",
		Channel:     "GoChan",
		PublishedAt: "2025-01-01T00:00:00Z",
	}
	if len(got) != 1 || got[0] != want {
		t.Errorf("got %+v, want [%+v]", got, want)
	}
}

func TestSearchVideosClampsMaxResults(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/youtube/v3/search", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("maxResults"); got != "50" {
			t.Errorf("maxResults %q, want capped value 50", got)
		}
		fmt.Fprint(w, `{"items": []}`)
	})
	if _, err := testClient(t, mux).SearchVideos(context.Background(), "go", 100); err != nil {
		t.Fatal(err)
	}
}

func TestGetVideo(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/youtube/v3/videos", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("id"); got != "vid1" {
			t.Errorf("id %q, want vid1", got)
		}
		fmt.Fprint(w, `{"items": [{
			"id": "vid1",
			"snippet": {"title": "Learn Go", "channelTitle": "GoChan", "description": "desc"},
			"contentDetails": {"duration": "PT1H2M3S"},
			"statistics": {"viewCount": "1000", "likeCount": "50"}
		}]}`)
	})

	got, err := testClient(t, mux).GetVideo(context.Background(), "vid1")
	if err != nil {
		t.Fatal(err)
	}
	want := Video{
		ID: "vid1", Title: "Learn Go", Channel: "GoChan",
		Duration: "1:02:03", Views: 1000, Likes: 50, Description: "desc",
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestGetVideoNotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/youtube/v3/videos", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"items": []}`)
	})
	_, err := testClient(t, mux).GetVideo(context.Background(), "ghost")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("want not-found error, got: %v", err)
	}
}

func TestHumanDuration(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "PT3M20S", want: "3:20"},
		{input: "PT1H2M3S", want: "1:02:03"},
		{input: "PT45S", want: "0:45"},
		{input: "PT2H", want: "2:00:00"},
		{input: "P1DT2H", want: "P1DT2H"},
	}
	for _, test := range tests {
		if got := humanDuration(test.input); got != test.want {
			t.Errorf("humanDuration(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}
