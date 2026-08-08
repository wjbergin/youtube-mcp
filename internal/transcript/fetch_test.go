package transcript

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestFetcher serves a player response pointing captions at the same test
// server, which serves the sample json3 document from parse_test.go.
func newTestFetcher(t *testing.T, playerJSON func(baseURL string) string) *Fetcher {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("/player", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("player called with %s, want POST", r.Method)
		}
		var body struct {
			VideoID string `json:"videoId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.VideoID == "" {
			t.Errorf("player request missing videoId: %v", err)
		}
		fmt.Fprint(w, playerJSON(srv.URL))
	})
	mux.HandleFunc("/timedtext", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("fmt") != "json3" {
			t.Errorf("timedtext fetched without fmt=json3: %s", r.URL)
		}
		fmt.Fprint(w, sampleJSON3)
	})
	return &Fetcher{HTTP: srv.Client(), PlayerURL: srv.URL + "/player"}
}

func TestFetchHappyPath(t *testing.T) {
	f := newTestFetcher(t, func(base string) string {
		return fmt.Sprintf(`{
			"playabilityStatus": {"status": "OK"},
			"captions": {"playerCaptionsTracklistRenderer": {"captionTracks": [
				{"baseUrl": "%s/timedtext?v=abc", "languageCode": "en"}
			]}}}`, base)
	})
	got, err := f.Fetch(context.Background(), "abc123", "en", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Hello world") {
		t.Errorf("transcript missing expected text: %q", got)
	}
}

func TestFetchUnavailableVideo(t *testing.T) {
	f := newTestFetcher(t, func(string) string {
		return `{"playabilityStatus": {"status": "ERROR", "reason": "Video unavailable"}}`
	})
	_, err := f.Fetch(context.Background(), "gone", "en", false)
	if err == nil || !strings.Contains(err.Error(), "Video unavailable") {
		t.Errorf("want unavailable error with reason, got: %v", err)
	}
}

func TestFetchNoCaptions(t *testing.T) {
	f := newTestFetcher(t, func(string) string {
		return `{"playabilityStatus": {"status": "OK"}}`
	})
	_, err := f.Fetch(context.Background(), "silent", "en", false)
	if err == nil || !strings.Contains(err.Error(), "no captions") {
		t.Errorf("want no-captions error, got: %v", err)
	}
}
