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
			Context struct {
				Client struct {
					ClientName string `json:"clientName"`
				} `json:"client"`
			} `json:"context"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.VideoID == "" {
			t.Errorf("player request missing videoId: %v", err)
		}
		// WEB-client player calls are PO-token-gated and report real videos as
		// unavailable, so the fetcher must identify as the ANDROID client.
		if name := body.Context.Client.ClientName; name != "ANDROID" {
			t.Errorf("player request client name = %q, want ANDROID", name)
		}
		fmt.Fprint(w, playerJSON(srv.URL))
	})
	mux.HandleFunc("/timedtext", func(w http.ResponseWriter, r *http.Request) {
		// Exactly one fmt param: YouTube honors the first occurrence, so an
		// appended fmt=json3 behind the track's own fmt=srv3 is ignored.
		if got := r.URL.Query()["fmt"]; len(got) != 1 || got[0] != "json3" {
			t.Errorf("timedtext fmt params = %v, want exactly [json3]: %s", got, r.URL)
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
				{"baseUrl": "%s/timedtext?v=abc&fmt=srv3", "languageCode": "en"}
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
