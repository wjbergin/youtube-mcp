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

// fakeYouTube stands in for YouTube's InnerTube and timedtext endpoints. The
// embedded *Fetcher is wired to it. Status fields default to 200 and may be
// set before calling Fetch to simulate an endpoint failing.
type fakeYouTube struct {
	*Fetcher
	playerStatus  int
	captionStatus int
	fetchedTrack  string // "v" param of the caption URL actually fetched
}

// newTestFetcher serves a player response pointing captions at the same test
// server, which serves the sample json3 document from parse_test.go.
func newTestFetcher(t *testing.T, playerJSON func(baseURL string) string) *fakeYouTube {
	t.Helper()
	yt := &fakeYouTube{}
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mux.HandleFunc("/player", func(w http.ResponseWriter, r *http.Request) {
		if yt.playerStatus != 0 {
			w.WriteHeader(yt.playerStatus)
			return
		}
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
		yt.fetchedTrack = r.URL.Query().Get("v")
		if yt.captionStatus != 0 {
			w.WriteHeader(yt.captionStatus)
			return
		}
		// Exactly one fmt param: YouTube honors the first occurrence, so an
		// appended fmt=json3 behind the track's own fmt=srv3 is ignored.
		if got := r.URL.Query()["fmt"]; len(got) != 1 || got[0] != "json3" {
			t.Errorf("timedtext fmt params = %v, want exactly [json3]: %s", got, r.URL)
		}
		fmt.Fprint(w, sampleJSON3)
	})
	yt.Fetcher = &Fetcher{HTTP: srv.Client(), PlayerURL: srv.URL + "/player"}
	return yt
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

func TestFetchZeroValueFieldsFallBackToDefaults(t *testing.T) {
	var f Fetcher
	if got := f.client(); got != http.DefaultClient {
		t.Errorf("client() = %v, want http.DefaultClient", got)
	}
	if got := f.playerURL(); got != defaultPlayerURL {
		t.Errorf("playerURL() = %q, want %q", got, defaultPlayerURL)
	}
}

func TestFetchWithNilHTTPClient(t *testing.T) {
	f := newTestFetcher(t, func(base string) string {
		return fmt.Sprintf(`{
			"playabilityStatus": {"status": "OK"},
			"captions": {"playerCaptionsTracklistRenderer": {"captionTracks": [
				{"baseUrl": "%s/timedtext?v=abc&fmt=srv3", "languageCode": "en"}
			]}}}`, base)
	})
	f.HTTP = nil // a &Fetcher{PlayerURL: ...} literal must not panic
	got, err := f.Fetch(context.Background(), "abc123", "en", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Hello world") {
		t.Errorf("transcript missing expected text: %q", got)
	}
}

func TestFetchWithTimestamps(t *testing.T) {
	f := newTestFetcher(t, func(base string) string {
		return fmt.Sprintf(`{
			"playabilityStatus": {"status": "OK"},
			"captions": {"playerCaptionsTracklistRenderer": {"captionTracks": [
				{"baseUrl": "%s/timedtext?v=abc&fmt=srv3", "languageCode": "en"}
			]}}}`, base)
	})
	got, err := f.Fetch(context.Background(), "abc123", "en", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[0:01] Hello world", "[1:05] second line", "[1:01:01] an hour in"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

func TestFetchSelectsRequestedTrackNotFirst(t *testing.T) {
	f := newTestFetcher(t, func(base string) string {
		return fmt.Sprintf(`{
			"playabilityStatus": {"status": "OK"},
			"captions": {"playerCaptionsTracklistRenderer": {"captionTracks": [
				{"baseUrl": "%[1]s/timedtext?v=de&fmt=srv3", "languageCode": "de"},
				{"baseUrl": "%[1]s/timedtext?v=en-asr&fmt=srv3", "languageCode": "en", "kind": "asr"},
				{"baseUrl": "%[1]s/timedtext?v=en&fmt=srv3", "languageCode": "en"}
			]}}}`, base)
	})
	if _, err := f.Fetch(context.Background(), "abc123", "en", false); err != nil {
		t.Fatal(err)
	}
	if f.fetchedTrack != "en" {
		t.Errorf("fetched track %q, want the human %q track", f.fetchedTrack, "en")
	}
}

func TestFetchPlayerEndpointError(t *testing.T) {
	f := newTestFetcher(t, func(string) string { return "" })
	f.playerStatus = http.StatusServiceUnavailable
	_, err := f.Fetch(context.Background(), "abc123", "en", false)
	if err == nil || !strings.Contains(err.Error(), "player endpoint returned") {
		t.Errorf("want player endpoint status error, got: %v", err)
	}
}

func TestFetchCaptionURLError(t *testing.T) {
	f := newTestFetcher(t, func(base string) string {
		return fmt.Sprintf(`{
			"playabilityStatus": {"status": "OK"},
			"captions": {"playerCaptionsTracklistRenderer": {"captionTracks": [
				{"baseUrl": "%s/timedtext?v=abc&fmt=srv3", "languageCode": "en"}
			]}}}`, base)
	})
	f.captionStatus = http.StatusForbidden
	_, err := f.Fetch(context.Background(), "abc123", "en", false)
	if err == nil || !strings.Contains(err.Error(), "caption URL returned") {
		t.Errorf("want caption URL status error, got: %v", err)
	}
}
