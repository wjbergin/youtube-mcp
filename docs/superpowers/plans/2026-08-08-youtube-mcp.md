# YouTube MCP Server Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Go MCP server (stdio) exposing 10 tools for YouTube playlist management (official Data API v3 + OAuth) and transcript fetching (unauthenticated InnerTube endpoint).

**Architecture:** `main.go` wires three internal packages: `internal/auth` (OAuth credential lifecycle), `internal/yt` (thin testable wrapper over the generated `youtube/v3` client), `internal/transcript` (isolated InnerTube caption fetcher). `internal/tools` defines the MCP tool surface against narrow interfaces so handlers are testable with fakes. Spec: `docs/superpowers/specs/2026-08-08-youtube-mcp-design.md`.

**Tech Stack:** Go 1.26, `github.com/modelcontextprotocol/go-sdk` (official MCP SDK), `google.golang.org/api/youtube/v3`, `golang.org/x/oauth2`.

**Implementation status (2026-08-08):** Tasks 1–12 are implemented on
`feat/implementation`. Build, vet, unit tests, race tests, the stdio MCP
handshake, and the read-only live transcript smoke test pass. Browser OAuth,
the account-mutating playlist smoke test, and Claude Code registration remain
explicit manual checks because they require the user's credentials/account.

**Conventions:** Module name is plain `youtube-mcp` (matches Bill's `indexer-go` convention). Tests use stdlib `testing` + `httptest` only — no assertion libraries. Every YouTube-API test drives the real generated client against an `httptest` server via `option.WithEndpoint`.

---

### Task 1: Project scaffold

**Files:**
- Create: `go.mod` (via commands)
- Create: `main.go`
- Create: `.gitignore`

- [x] **Step 1: Init module and fetch dependencies**

```bash
cd /Users/bill/work/youtube-mcp
go mod init youtube-mcp
go get github.com/modelcontextprotocol/go-sdk@latest
go get google.golang.org/api@latest
go get golang.org/x/oauth2@latest
```

Expected: `go.mod` created listing all three dependencies.

- [x] **Step 2: Write placeholder main.go** (replaced in Task 11)

```go
// youtube-mcp is an MCP server for managing YouTube playlists and fetching
// video transcripts.
package main

import "fmt"

func main() {
	fmt.Println("youtube-mcp: not yet implemented")
}
```

- [x] **Step 3: Write .gitignore**

```gitignore
/youtube-mcp
```

(The built binary lands in the repo root with the same name as the module.)

- [x] **Step 4: Verify it builds**

Run: `go build ./... && go vet ./...`
Expected: no output, exit 0.

- [x] **Step 5: Commit**

```bash
git add go.mod go.sum main.go .gitignore
git commit -m "chore: scaffold Go module with MCP and YouTube API dependencies"
```

---

### Task 2: Transcript json3 parser

**Files:**
- Create: `internal/transcript/parse.go`
- Test: `internal/transcript/parse_test.go`

YouTube caption tracks fetched with `fmt=json3` return `{"events":[{"tStartMs":N,"segs":[{"utf8":"text"}]}]}`. Events without `segs` are styling/window events and must be skipped. Newlines inside segments become spaces.

- [x] **Step 1: Write the failing test**

```go
package transcript

import (
	"strings"
	"testing"
)

const sampleJSON3 = `{
  "events": [
    {"tStartMs": 0, "dDurationMs": 1000},
    {"tStartMs": 1000, "segs": [{"utf8": "Hello"}, {"utf8": " world"}]},
    {"tStartMs": 65000, "segs": [{"utf8": "second\nline"}]},
    {"tStartMs": 3661000, "segs": [{"utf8": "an hour in"}]}
  ]
}`

func TestParseJSON3PlainText(t *testing.T) {
	got, err := parseJSON3([]byte(sampleJSON3), false)
	if err != nil {
		t.Fatal(err)
	}
	want := "Hello world\nsecond line\nan hour in"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestParseJSON3WithTimestamps(t *testing.T) {
	got, err := parseJSON3([]byte(sampleJSON3), true)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[0:01] Hello world", "[1:05] second line", "[1:01:01] an hour in"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

func TestParseJSON3Empty(t *testing.T) {
	if _, err := parseJSON3([]byte(`{"events":[]}`), false); err == nil {
		t.Error("expected error for empty caption track")
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/transcript/`
Expected: FAIL — `undefined: parseJSON3`

- [x] **Step 3: Write the implementation**

```go
package transcript

import (
	"encoding/json"
	"fmt"
	"strings"
)

type json3Doc struct {
	Events []json3Event `json:"events"`
}

type json3Event struct {
	StartMs int64      `json:"tStartMs"`
	Segs    []json3Seg `json:"segs"`
}

type json3Seg struct {
	UTF8 string `json:"utf8"`
}

// parseJSON3 flattens a json3 caption document into plain text, one line per
// caption event. With timestamps, each line is prefixed [m:ss] or [h:mm:ss].
func parseJSON3(data []byte, withTimestamps bool) (string, error) {
	var doc json3Doc
	if err := json.Unmarshal(data, &doc); err != nil {
		return "", fmt.Errorf("parsing caption data: %w", err)
	}
	var lines []string
	for _, ev := range doc.Events {
		var b strings.Builder
		for _, seg := range ev.Segs {
			b.WriteString(seg.UTF8)
		}
		text := strings.TrimSpace(strings.ReplaceAll(b.String(), "\n", " "))
		if text == "" {
			continue
		}
		if withTimestamps {
			text = fmt.Sprintf("[%s] %s", formatTimestamp(ev.StartMs), text)
		}
		lines = append(lines, text)
	}
	if len(lines) == 0 {
		return "", fmt.Errorf("caption track was empty")
	}
	return strings.Join(lines, "\n"), nil
}

func formatTimestamp(ms int64) string {
	total := ms / 1000
	h, m, s := total/3600, (total%3600)/60, total%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/transcript/`
Expected: `ok  youtube-mcp/internal/transcript`

- [x] **Step 5: Commit**

```bash
git add internal/transcript/
git commit -m "feat: parse json3 caption documents into plain text"
```

---

### Task 3: Caption track selection

**Files:**
- Create: `internal/transcript/select.go`
- Test: `internal/transcript/select_test.go`

Rules from the spec: match language ignoring region subtags (`en` matches `en-GB`), prefer human-made captions over auto-generated (`kind == "asr"`), and when the language is missing, the error must list what IS available.

- [x] **Step 1: Write the failing test**

```go
package transcript

import (
	"strings"
	"testing"
)

func TestSelectTrackPrefersHumanOverASR(t *testing.T) {
	tracks := []captionTrack{
		{BaseURL: "http://x/asr", LanguageCode: "en", Kind: "asr"},
		{BaseURL: "http://x/manual", LanguageCode: "en"},
	}
	got, err := selectTrack(tracks, "en")
	if err != nil {
		t.Fatal(err)
	}
	if got.BaseURL != "http://x/manual" {
		t.Errorf("picked %q, want the manual track", got.BaseURL)
	}
}

func TestSelectTrackFallsBackToASR(t *testing.T) {
	tracks := []captionTrack{{BaseURL: "http://x/asr", LanguageCode: "en", Kind: "asr"}}
	got, err := selectTrack(tracks, "en")
	if err != nil {
		t.Fatal(err)
	}
	if got.BaseURL != "http://x/asr" {
		t.Errorf("picked %q, want the asr track", got.BaseURL)
	}
}

func TestSelectTrackIgnoresRegionSubtag(t *testing.T) {
	tracks := []captionTrack{{BaseURL: "http://x/gb", LanguageCode: "en-GB"}}
	if _, err := selectTrack(tracks, "en"); err != nil {
		t.Errorf("en should match en-GB, got error: %v", err)
	}
}

func TestSelectTrackMissingLanguageListsAvailable(t *testing.T) {
	tracks := []captionTrack{
		{LanguageCode: "de"},
		{LanguageCode: "fr", Kind: "asr"},
	}
	_, err := selectTrack(tracks, "en")
	if err == nil {
		t.Fatal("expected error for missing language")
	}
	for _, want := range []string{"de", "fr (auto-generated)"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}

func TestSelectTrackNoCaptions(t *testing.T) {
	if _, err := selectTrack(nil, "en"); err == nil {
		t.Error("expected error for no captions")
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/transcript/`
Expected: FAIL — `undefined: captionTrack`, `undefined: selectTrack`

- [x] **Step 3: Write the implementation**

```go
package transcript

import (
	"fmt"
	"sort"
	"strings"
)

type captionTrack struct {
	BaseURL      string `json:"baseUrl"`
	LanguageCode string `json:"languageCode"`
	Kind         string `json:"kind"` // "asr" means auto-generated
}

// selectTrack picks the caption track for lang, preferring human-made captions
// over auto-generated ones. Language matching ignores region subtags, so
// lang "en" matches a track coded "en-GB".
func selectTrack(tracks []captionTrack, lang string) (captionTrack, error) {
	if len(tracks) == 0 {
		return captionTrack{}, fmt.Errorf("video has no captions")
	}
	var matches []captionTrack
	for _, tr := range tracks {
		if baseLang(tr.LanguageCode) == baseLang(lang) {
			matches = append(matches, tr)
		}
	}
	if len(matches) == 0 {
		return captionTrack{}, fmt.Errorf("no %q captions for this video; available: %s", lang, availableLanguages(tracks))
	}
	for _, tr := range matches {
		if tr.Kind != "asr" {
			return tr, nil
		}
	}
	return matches[0], nil
}

func baseLang(code string) string {
	return strings.ToLower(strings.SplitN(code, "-", 2)[0])
}

func availableLanguages(tracks []captionTrack) string {
	var langs []string
	for _, tr := range tracks {
		l := tr.LanguageCode
		if tr.Kind == "asr" {
			l += " (auto-generated)"
		}
		langs = append(langs, l)
	}
	sort.Strings(langs)
	return strings.Join(langs, ", ")
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/transcript/`
Expected: `ok  youtube-mcp/internal/transcript`

- [x] **Step 5: Commit**

```bash
git add internal/transcript/
git commit -m "feat: select caption track by language, preferring human captions"
```

---

### Task 4: Transcript fetcher (InnerTube)

**Files:**
- Create: `internal/transcript/fetch.go`
- Test: `internal/transcript/fetch_test.go`

The fetcher POSTs to the InnerTube `player` endpoint (no auth), reads `captions.playerCaptionsTracklistRenderer.captionTracks`, then GETs the chosen track's `baseUrl` with `fmt=json3`. Both URLs are overridable so tests hit an `httptest` server.

> **Amendment (2026-08-08, during execution):** live probing showed the WEB client context returns UNPLAYABLE (PO-token gating) — the shipped code uses the **ANDROID client context** (`clientName: ANDROID, clientVersion: 20.10.38, androidSdkVersion: 30, hl: en, gl: US`) instead of the WEB context shown below. Additionally, ANDROID caption `baseUrl`s already carry `fmt=srv3` and YouTube honors the first `fmt` param, so the shipped code **replaces** `fmt` via `net/url` rather than appending `&fmt=json3` as shown below. The code blocks below are the original pre-execution plan.

- [x] **Step 1: Write the failing test**

```go
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
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/transcript/`
Expected: FAIL — `undefined: Fetcher`

- [x] **Step 3: Write the implementation**

```go
package transcript

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const defaultPlayerURL = "https://www.youtube.com/youtubei/v1/player"

// Fetcher retrieves transcripts via YouTube's InnerTube player endpoint.
// The endpoint is unofficial and unauthenticated; all knowledge of it is
// confined to this package so a breakage is a one-package fix.
type Fetcher struct {
	HTTP      *http.Client
	PlayerURL string
}

func New() *Fetcher {
	return &Fetcher{HTTP: http.DefaultClient, PlayerURL: defaultPlayerURL}
}

type playerResponse struct {
	PlayabilityStatus struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	} `json:"playabilityStatus"`
	Captions struct {
		Renderer struct {
			CaptionTracks []captionTrack `json:"captionTracks"`
		} `json:"playerCaptionsTracklistRenderer"`
	} `json:"captions"`
}

func (f *Fetcher) Fetch(ctx context.Context, videoID, lang string, withTimestamps bool) (string, error) {
	tracks, err := f.fetchTracks(ctx, videoID)
	if err != nil {
		return "", err
	}
	track, err := selectTrack(tracks, lang)
	if err != nil {
		return "", err
	}
	sep := "?"
	if strings.Contains(track.BaseURL, "?") {
		sep = "&"
	}
	data, err := f.get(ctx, track.BaseURL+sep+"fmt=json3")
	if err != nil {
		return "", fmt.Errorf("fetching caption track: %w", err)
	}
	return parseJSON3(data, withTimestamps)
}

func (f *Fetcher) fetchTracks(ctx context.Context, videoID string) ([]captionTrack, error) {
	body, err := json.Marshal(map[string]any{
		"context": map[string]any{
			"client": map[string]any{
				"clientName":    "WEB",
				"clientVersion": "2.20250101.00.00",
			},
		},
		"videoId": videoID,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.PlayerURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := f.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("calling player endpoint: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("player endpoint returned %s", resp.Status)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var pr playerResponse
	if err := json.Unmarshal(data, &pr); err != nil {
		return nil, fmt.Errorf("parsing player response: %w", err)
	}
	if s := pr.PlayabilityStatus.Status; s != "" && s != "OK" {
		reason := pr.PlayabilityStatus.Reason
		if reason == "" {
			reason = s
		}
		return nil, fmt.Errorf("video unavailable or restricted: %s", reason)
	}
	if len(pr.Captions.Renderer.CaptionTracks) == 0 {
		return nil, fmt.Errorf("video has no captions")
	}
	return pr.Captions.Renderer.CaptionTracks, nil
}

func (f *Fetcher) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := f.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("caption URL returned %s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/transcript/`
Expected: `ok  youtube-mcp/internal/transcript`

- [x] **Step 5: Commit**

```bash
git add internal/transcript/
git commit -m "feat: fetch transcripts via InnerTube player endpoint"
```

---

### Task 5: YouTube API error mapping

**Files:**
- Create: `internal/yt/errors.go`
- Test: `internal/yt/errors_test.go`

Per the spec's error table: quota exhaustion, 404, and auth failures must come back as plain-language, actionable messages. Everything else passes through with code + message.

- [x] **Step 1: Write the failing test**

```go
package yt

import (
	"fmt"
	"strings"
	"testing"

	"google.golang.org/api/googleapi"
)

func TestFriendlyErrorQuota(t *testing.T) {
	err := friendlyError(&googleapi.Error{
		Code:   403,
		Errors: []googleapi.ErrorItem{{Reason: "quotaExceeded"}},
	})
	if !strings.Contains(err.Error(), "quota") || !strings.Contains(err.Error(), "midnight Pacific") {
		t.Errorf("quota error not actionable: %v", err)
	}
}

func TestFriendlyErrorNotFound(t *testing.T) {
	err := friendlyError(&googleapi.Error{Code: 404})
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("404 not mapped: %v", err)
	}
}

func TestFriendlyErrorAuth(t *testing.T) {
	err := friendlyError(&googleapi.Error{Code: 401})
	if !strings.Contains(err.Error(), "youtube-mcp auth") {
		t.Errorf("401 should tell the user to re-auth: %v", err)
	}
}

func TestFriendlyErrorInvalidGrant(t *testing.T) {
	err := friendlyError(fmt.Errorf(`oauth2: "invalid_grant" "Token has been revoked"`))
	if !strings.Contains(err.Error(), "youtube-mcp auth") {
		t.Errorf("invalid_grant should tell the user to re-auth: %v", err)
	}
}

func TestFriendlyErrorWrapped(t *testing.T) {
	inner := &googleapi.Error{Code: 404}
	err := friendlyError(fmt.Errorf("listing playlists: %w", inner))
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("wrapped googleapi.Error not detected: %v", err)
	}
}

func TestFriendlyErrorNil(t *testing.T) {
	if friendlyError(nil) != nil {
		t.Error("nil must map to nil")
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/yt/`
Expected: FAIL — `undefined: friendlyError`

- [x] **Step 3: Write the implementation**

```go
// Package yt wraps the generated YouTube Data API client behind a small,
// testable surface. All quota-consuming calls live here.
package yt

import (
	"errors"
	"fmt"
	"strings"

	"google.golang.org/api/googleapi"
)

// friendlyError rewrites Google API failures into messages that tell the
// model (and the user) what to actually do next.
func friendlyError(err error) error {
	if err == nil {
		return nil
	}
	var gerr *googleapi.Error
	if errors.As(err, &gerr) {
		switch {
		case gerr.Code == 403 && hasReason(gerr, "quotaExceeded"):
			return fmt.Errorf("YouTube API daily quota exhausted; it resets at midnight Pacific time")
		case gerr.Code == 404:
			return fmt.Errorf("not found: no playlist or video with that id")
		case gerr.Code == 401:
			return fmt.Errorf("authentication failed: run `youtube-mcp auth` to sign in again")
		}
		return fmt.Errorf("YouTube API error %d: %s", gerr.Code, gerr.Message)
	}
	if strings.Contains(err.Error(), "invalid_grant") {
		return fmt.Errorf("authentication token expired or revoked: run `youtube-mcp auth` to sign in again")
	}
	return err
}

func hasReason(gerr *googleapi.Error, reason string) bool {
	for _, e := range gerr.Errors {
		if e.Reason == reason {
			return true
		}
	}
	return false
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/yt/`
Expected: `ok  youtube-mcp/internal/yt`

- [x] **Step 5: Commit**

```bash
git add internal/yt/
git commit -m "feat: map YouTube API errors to actionable messages"
```

---

### Task 6: Client wrapper + playlist CRUD

**Files:**
- Create: `internal/yt/client.go`
- Create: `internal/yt/playlists.go`
- Test: `internal/yt/playlists_test.go`

The generated client is pointed at an `httptest` server via `option.WithEndpoint`. With the endpoint overridden, the client requests paths like `/youtube/v3/playlists` — register mux handlers on those exact paths. The key behavior to test is **update-merge**: the API replaces the whole snippet on update, so `UpdatePlaylist` must first fetch the current snippet and only overwrite the fields the caller provided.

- [x] **Step 1: Write client.go (types + constructor — no test yet, exercised by every test in this package)**

```go
package yt

import (
	"context"
	"net/http"

	"google.golang.org/api/option"
	ytapi "google.golang.org/api/youtube/v3"
)

type Client struct {
	svc *ytapi.Service
}

// New builds a Client around hc (an authenticated *http.Client in production).
// Tests pass option.WithEndpoint to aim the generated client at a fake server.
func New(ctx context.Context, hc *http.Client, opts ...option.ClientOption) (*Client, error) {
	opts = append([]option.ClientOption{option.WithHTTPClient(hc)}, opts...)
	svc, err := ytapi.NewService(ctx, opts...)
	if err != nil {
		return nil, err
	}
	return &Client{svc: svc}, nil
}

type Playlist struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Privacy     string `json:"privacy"`
	ItemCount   int64  `json:"item_count"`
	URL         string `json:"url"`
}

type PlaylistItem struct {
	ID       string `json:"playlist_item_id"`
	VideoID  string `json:"video_id"`
	Title    string `json:"title"`
	Position int64  `json:"position"`
}

type SearchResult struct {
	VideoID     string `json:"video_id"`
	Title       string `json:"title"`
	Channel     string `json:"channel"`
	PublishedAt string `json:"published_at"`
}

type Video struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Channel     string `json:"channel"`
	Duration    string `json:"duration"`
	Views       uint64 `json:"views"`
	Likes       uint64 `json:"likes"`
	Description string `json:"description,omitempty"`
}

func playlistURL(id string) string {
	return "https://www.youtube.com/playlist?list=" + id
}
```

- [x] **Step 2: Write the failing test**

```go
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
```

- [x] **Step 3: Run test to verify it fails**

Run: `go test ./internal/yt/`
Expected: FAIL — `undefined: New` (and the playlist methods)

- [x] **Step 4: Write playlists.go**

```go
package yt

import (
	"context"
	"fmt"

	ytapi "google.golang.org/api/youtube/v3"
)

func (c *Client) ListPlaylists(ctx context.Context, maxResults int64) ([]Playlist, error) {
	if maxResults <= 0 {
		maxResults = 50
	}
	resp, err := c.svc.Playlists.List([]string{"snippet", "status", "contentDetails"}).
		Mine(true).MaxResults(maxResults).Context(ctx).Do()
	if err != nil {
		return nil, friendlyError(err)
	}
	out := make([]Playlist, 0, len(resp.Items))
	for _, p := range resp.Items {
		out = append(out, fromAPIPlaylist(p))
	}
	return out, nil
}

func (c *Client) CreatePlaylist(ctx context.Context, title, description, privacy string) (Playlist, error) {
	if privacy == "" {
		privacy = "private"
	}
	p := &ytapi.Playlist{
		Snippet: &ytapi.PlaylistSnippet{Title: title, Description: description},
		Status:  &ytapi.PlaylistStatus{PrivacyStatus: privacy},
	}
	created, err := c.svc.Playlists.Insert([]string{"snippet", "status"}, p).Context(ctx).Do()
	if err != nil {
		return Playlist{}, friendlyError(err)
	}
	return fromAPIPlaylist(created), nil
}

// UpdatePlaylist merges the provided fields into the playlist's current
// state: the API replaces the whole snippet on update, so unchanged fields
// must be re-sent with their existing values. nil means "leave unchanged".
func (c *Client) UpdatePlaylist(ctx context.Context, id string, title, description, privacy *string) (Playlist, error) {
	resp, err := c.svc.Playlists.List([]string{"snippet", "status"}).Id(id).Context(ctx).Do()
	if err != nil {
		return Playlist{}, friendlyError(err)
	}
	if len(resp.Items) == 0 {
		return Playlist{}, fmt.Errorf("not found: no playlist with id %q", id)
	}
	cur := resp.Items[0]
	if title != nil {
		cur.Snippet.Title = *title
	}
	if description != nil {
		cur.Snippet.Description = *description
	}
	if privacy != nil {
		if cur.Status == nil {
			cur.Status = &ytapi.PlaylistStatus{}
		}
		cur.Status.PrivacyStatus = *privacy
	}
	updated, err := c.svc.Playlists.Update([]string{"snippet", "status"}, cur).Context(ctx).Do()
	if err != nil {
		return Playlist{}, friendlyError(err)
	}
	return fromAPIPlaylist(updated), nil
}

func (c *Client) DeletePlaylist(ctx context.Context, id string) error {
	return friendlyError(c.svc.Playlists.Delete(id).Context(ctx).Do())
}

func fromAPIPlaylist(p *ytapi.Playlist) Playlist {
	out := Playlist{ID: p.Id, URL: playlistURL(p.Id)}
	if p.Snippet != nil {
		out.Title = p.Snippet.Title
		out.Description = p.Snippet.Description
	}
	if p.Status != nil {
		out.Privacy = p.Status.PrivacyStatus
	}
	if p.ContentDetails != nil {
		out.ItemCount = p.ContentDetails.ItemCount
	}
	return out
}
```

- [x] **Step 5: Run test to verify it passes**

Run: `go test ./internal/yt/`
Expected: `ok  youtube-mcp/internal/yt`

- [x] **Step 6: Commit**

```bash
git add internal/yt/
git commit -m "feat: playlist CRUD with merge-on-update semantics"
```

---

### Task 7: Playlist items (list, add, remove-all-occurrences)

**Files:**
- Create: `internal/yt/items.go`
- Test: `internal/yt/items_test.go`

`RemoveVideo` takes a video id (not a playlist-item id), pages through the whole playlist to find every occurrence, deletes them all, and returns the count. A video not present is an error, not a silent zero. Adding at an explicit position 0 requires `ForceSendFields` because the generated struct tags are `omitempty`.

- [x] **Step 1: Write the failing test**

```go
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
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/yt/`
Expected: FAIL — `undefined` methods `ListPlaylistItems`, `AddVideo`, `RemoveVideo`

- [x] **Step 3: Write items.go**

```go
package yt

import (
	"context"
	"fmt"

	ytapi "google.golang.org/api/youtube/v3"
)

func (c *Client) ListPlaylistItems(ctx context.Context, playlistID string, maxResults int64, pageToken string) ([]PlaylistItem, string, error) {
	if maxResults <= 0 {
		maxResults = 50
	}
	call := c.svc.PlaylistItems.List([]string{"snippet"}).
		PlaylistId(playlistID).MaxResults(maxResults).Context(ctx)
	if pageToken != "" {
		call = call.PageToken(pageToken)
	}
	resp, err := call.Do()
	if err != nil {
		return nil, "", friendlyError(err)
	}
	out := make([]PlaylistItem, 0, len(resp.Items))
	for _, it := range resp.Items {
		out = append(out, fromAPIItem(it))
	}
	return out, resp.NextPageToken, nil
}

func (c *Client) AddVideo(ctx context.Context, playlistID, videoID string, position *int64) (PlaylistItem, error) {
	snippet := &ytapi.PlaylistItemSnippet{
		PlaylistId: playlistID,
		ResourceId: &ytapi.ResourceId{Kind: "youtube#video", VideoId: videoID},
	}
	if position != nil {
		snippet.Position = *position
		snippet.ForceSendFields = append(snippet.ForceSendFields, "Position")
	}
	created, err := c.svc.PlaylistItems.Insert([]string{"snippet"}, &ytapi.PlaylistItem{Snippet: snippet}).Context(ctx).Do()
	if err != nil {
		return PlaylistItem{}, friendlyError(err)
	}
	return fromAPIItem(created), nil
}

// RemoveVideo deletes every occurrence of videoID in the playlist and returns
// how many entries were removed. A video not present is an error.
func (c *Client) RemoveVideo(ctx context.Context, playlistID, videoID string) (int, error) {
	var ids []string
	pageToken := ""
	for {
		items, next, err := c.ListPlaylistItems(ctx, playlistID, 50, pageToken)
		if err != nil {
			return 0, err
		}
		for _, it := range items {
			if it.VideoID == videoID {
				ids = append(ids, it.ID)
			}
		}
		if next == "" {
			break
		}
		pageToken = next
	}
	if len(ids) == 0 {
		return 0, fmt.Errorf("video %s is not in playlist %s", videoID, playlistID)
	}
	for _, id := range ids {
		if err := c.svc.PlaylistItems.Delete(id).Context(ctx).Do(); err != nil {
			return 0, friendlyError(err)
		}
	}
	return len(ids), nil
}

func fromAPIItem(it *ytapi.PlaylistItem) PlaylistItem {
	out := PlaylistItem{ID: it.Id}
	if it.Snippet != nil {
		out.Title = it.Snippet.Title
		out.Position = it.Snippet.Position
		if it.Snippet.ResourceId != nil {
			out.VideoID = it.Snippet.ResourceId.VideoId
		}
	}
	return out
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/yt/`
Expected: `ok  youtube-mcp/internal/yt`

- [x] **Step 5: Commit**

```bash
git add internal/yt/
git commit -m "feat: playlist item listing, adding, and remove-all-occurrences"
```

---

### Task 8: Search and video details

**Files:**
- Create: `internal/yt/videos.go`
- Test: `internal/yt/videos_test.go`

`GetVideo` converts the API's ISO 8601 duration (`PT1H2M3S`) to a human-readable `1:02:03`. `SearchVideos` clamps `max_results` to [1, 50] with a default of 10 (search costs 100 quota units — don't over-fetch by default).

- [x] **Step 1: Write the failing test**

```go
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
		q := r.URL.Query()
		if q.Get("q") != "go tutorials" || q.Get("type") != "video" {
			t.Errorf("bad query: %s", r.URL)
		}
		if q.Get("maxResults") != "10" {
			t.Errorf("maxResults %q, want default 10", q.Get("maxResults"))
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
	want := SearchResult{VideoID: "vid1", Title: "Learn Go", Channel: "GoChan", PublishedAt: "2025-01-01T00:00:00Z"}
	if len(got) != 1 || got[0] != want {
		t.Errorf("got %+v, want [%+v]", got, want)
	}
}

func TestGetVideo(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/youtube/v3/videos", func(w http.ResponseWriter, r *http.Request) {
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
	want := Video{ID: "vid1", Title: "Learn Go", Channel: "GoChan",
		Duration: "1:02:03", Views: 1000, Likes: 50, Description: "desc"}
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
	cases := map[string]string{
		"PT3M20S": "3:20",
		"PT1H2M3S": "1:02:03",
		"PT45S":   "0:45",
		"PT2H":    "2:00:00",
		"P1DT2H":  "P1DT2H", // unparseable passes through untouched
	}
	for in, want := range cases {
		if got := humanDuration(in); got != want {
			t.Errorf("humanDuration(%q) = %q, want %q", in, got, want)
		}
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/yt/`
Expected: FAIL — `undefined: humanDuration` and the two methods

- [x] **Step 3: Write videos.go**

```go
package yt

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
)

func (c *Client) SearchVideos(ctx context.Context, query string, maxResults int64) ([]SearchResult, error) {
	if maxResults <= 0 {
		maxResults = 10
	}
	if maxResults > 50 {
		maxResults = 50
	}
	resp, err := c.svc.Search.List([]string{"snippet"}).
		Q(query).Type("video").MaxResults(maxResults).Context(ctx).Do()
	if err != nil {
		return nil, friendlyError(err)
	}
	out := make([]SearchResult, 0, len(resp.Items))
	for _, it := range resp.Items {
		r := SearchResult{}
		if it.Id != nil {
			r.VideoID = it.Id.VideoId
		}
		if it.Snippet != nil {
			r.Title = it.Snippet.Title
			r.Channel = it.Snippet.ChannelTitle
			r.PublishedAt = it.Snippet.PublishedAt
		}
		out = append(out, r)
	}
	return out, nil
}

func (c *Client) GetVideo(ctx context.Context, videoID string) (Video, error) {
	resp, err := c.svc.Videos.List([]string{"snippet", "contentDetails", "statistics"}).
		Id(videoID).Context(ctx).Do()
	if err != nil {
		return Video{}, friendlyError(err)
	}
	if len(resp.Items) == 0 {
		return Video{}, fmt.Errorf("not found: no video with id %q", videoID)
	}
	v := resp.Items[0]
	out := Video{ID: v.Id}
	if v.Snippet != nil {
		out.Title = v.Snippet.Title
		out.Channel = v.Snippet.ChannelTitle
		out.Description = v.Snippet.Description
	}
	if v.ContentDetails != nil {
		out.Duration = humanDuration(v.ContentDetails.Duration)
	}
	if v.Statistics != nil {
		out.Views = v.Statistics.ViewCount
		out.Likes = v.Statistics.LikeCount
	}
	return out, nil
}

var isoDuration = regexp.MustCompile(`^PT(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?$`)

// humanDuration converts ISO 8601 durations like PT1H2M3S to 1:02:03.
// Unparseable input (e.g. multi-day P1DT2H) is returned as-is.
func humanDuration(iso string) string {
	m := isoDuration.FindStringSubmatch(iso)
	if m == nil {
		return iso
	}
	h, _ := strconv.Atoi(zeroIfEmpty(m[1]))
	min, _ := strconv.Atoi(zeroIfEmpty(m[2]))
	s, _ := strconv.Atoi(zeroIfEmpty(m[3]))
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, min, s)
	}
	return fmt.Sprintf("%d:%02d", min, s)
}

func zeroIfEmpty(s string) string {
	if s == "" {
		return "0"
	}
	return s
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/yt/`
Expected: `ok  youtube-mcp/internal/yt`

- [x] **Step 5: Commit**

```bash
git add internal/yt/
git commit -m "feat: video search and details with human-readable durations"
```

---

### Task 9: OAuth credential lifecycle

**Files:**
- Create: `internal/auth/auth.go`
- Test: `internal/auth/auth_test.go`

`Dir` is a package variable so tests can point it at `t.TempDir()`. The browser consent flow itself can't be unit-tested — it's covered by the manual smoke test in Task 12. Token round-trip, missing-file messages, and file permissions ARE unit-tested.

- [x] **Step 1: Write the failing test**

```go
package auth

import (
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func withTempDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	orig := Dir
	Dir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { Dir = orig })
}

func TestTokenRoundTrip(t *testing.T) {
	withTempDir(t)
	tok := &oauth2.Token{
		AccessToken:  "at",
		RefreshToken: "rt",
		Expiry:       time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := SaveToken(tok); err != nil {
		t.Fatal(err)
	}
	got, err := LoadToken()
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "at" || got.RefreshToken != "rt" || !got.Expiry.Equal(tok.Expiry) {
		t.Errorf("got %+v", got)
	}
}

func TestLoadTokenMissingTellsUserToAuth(t *testing.T) {
	withTempDir(t)
	_, err := LoadToken()
	if err == nil || !strings.Contains(err.Error(), "youtube-mcp auth") {
		t.Errorf("missing token must instruct re-auth, got: %v", err)
	}
}

func TestLoadOAuthConfigMissingMentionsConsole(t *testing.T) {
	withTempDir(t)
	_, err := LoadOAuthConfig()
	if err == nil || !strings.Contains(err.Error(), "credentials.json") {
		t.Errorf("missing credentials must point at setup docs, got: %v", err)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/auth/`
Expected: FAIL — `undefined: Dir`, `SaveToken`, `LoadToken`, `LoadOAuthConfig`

- [x] **Step 3: Write the implementation**

```go
// Package auth handles the OAuth credential lifecycle: a one-time browser
// consent flow (`youtube-mcp auth`) and a cached, auto-refreshing token
// thereafter.
package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	ytapi "google.golang.org/api/youtube/v3"
)

// Dir locates the config directory; a variable so tests can redirect it.
var Dir = func() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "youtube-mcp"), nil
}

func credentialsPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "credentials.json"), nil
}

func tokenPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "token.json"), nil
}

func LoadOAuthConfig() (*oauth2.Config, error) {
	p, err := credentialsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("no OAuth credentials at %s: create a Desktop-app OAuth client in Google Cloud Console and save its JSON there (see README, credentials.json)", p)
	}
	cfg, err := google.ConfigFromJSON(data, ytapi.YoutubeScope)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", p, err)
	}
	return cfg, nil
}

func LoadToken() (*oauth2.Token, error) {
	p, err := tokenPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("not authenticated: run `youtube-mcp auth` to sign in")
	}
	var tok oauth2.Token
	if err := json.Unmarshal(data, &tok); err != nil {
		return nil, fmt.Errorf("corrupt token at %s: delete it and run `youtube-mcp auth` again", p)
	}
	return &tok, nil
}

func SaveToken(tok *oauth2.Token) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(tok)
	if err != nil {
		return err
	}
	p, err := tokenPath()
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

// HTTPClient returns an authenticated, auto-refreshing HTTP client, or an
// error telling the user how to authenticate.
func HTTPClient(ctx context.Context) (*http.Client, error) {
	cfg, err := LoadOAuthConfig()
	if err != nil {
		return nil, err
	}
	tok, err := LoadToken()
	if err != nil {
		return nil, err
	}
	return cfg.Client(ctx, tok), nil
}

// RunAuthFlow performs the one-time browser consent flow and saves the token.
func RunAuthFlow(ctx context.Context) error {
	cfg, err := LoadOAuthConfig()
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer ln.Close()
	cfg.RedirectURL = fmt.Sprintf("http://%s/", ln.Addr().String())

	codeCh := make(chan string, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			return
		}
		fmt.Fprintln(w, "Authorized. You can close this tab.")
		codeCh <- code
	})}
	go srv.Serve(ln)
	defer srv.Close()

	// AccessTypeOffline is required to get a refresh token; ApprovalForce
	// ensures one is issued even on re-authentication.
	url := cfg.AuthCodeURL("state", oauth2.AccessTypeOffline, oauth2.ApprovalForce)
	fmt.Fprintf(os.Stderr, "Opening browser for authorization...\nIf it doesn't open, visit:\n%s\n", url)
	openBrowser(url)

	var code string
	select {
	case code = <-codeCh:
	case <-ctx.Done():
		return ctx.Err()
	}
	tok, err := cfg.Exchange(ctx, code)
	if err != nil {
		return fmt.Errorf("exchanging authorization code: %w", err)
	}
	if err := SaveToken(tok); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "Token saved. youtube-mcp is ready to use.")
	return nil
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		return
	}
	_ = cmd.Start()
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/auth/`
Expected: `ok  youtube-mcp/internal/auth`

- [x] **Step 5: Commit**

```bash
git add internal/auth/
git commit -m "feat: OAuth credential lifecycle with loopback consent flow"
```

---

### Task 10: MCP tool surface

**Files:**
- Create: `internal/tools/tools.go`
- Test: `internal/tools/tools_test.go`

Handlers depend on a `Service` interface (implemented by `*yt.Client`) obtained through a `Provider` func, so the server can start unauthenticated and return instructive errors until `youtube-mcp auth` has been run. Tests connect a real MCP client over `mcp.NewInMemoryTransports()`, which also validates schema generation end to end.

- [x] **Step 1: Write the failing test**

```go
package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"youtube-mcp/internal/yt"
)

// fakeService implements Service in memory.
type fakeService struct {
	playlists []yt.Playlist
	removed   int
}

func (f *fakeService) ListPlaylists(ctx context.Context, maxResults int64) ([]yt.Playlist, error) {
	return f.playlists, nil
}
func (f *fakeService) CreatePlaylist(ctx context.Context, title, description, privacy string) (yt.Playlist, error) {
	return yt.Playlist{ID: "PLnew", Title: title, Privacy: "private"}, nil
}
func (f *fakeService) UpdatePlaylist(ctx context.Context, id string, title, description, privacy *string) (yt.Playlist, error) {
	return yt.Playlist{ID: id, Title: "Updated"}, nil
}
func (f *fakeService) DeletePlaylist(ctx context.Context, id string) error { return nil }
func (f *fakeService) ListPlaylistItems(ctx context.Context, playlistID string, maxResults int64, pageToken string) ([]yt.PlaylistItem, string, error) {
	return []yt.PlaylistItem{{ID: "i1", VideoID: "v1", Title: "T"}}, "", nil
}
func (f *fakeService) AddVideo(ctx context.Context, playlistID, videoID string, position *int64) (yt.PlaylistItem, error) {
	return yt.PlaylistItem{ID: "i2", VideoID: videoID}, nil
}
func (f *fakeService) RemoveVideo(ctx context.Context, playlistID, videoID string) (int, error) {
	return f.removed, nil
}
func (f *fakeService) SearchVideos(ctx context.Context, query string, maxResults int64) ([]yt.SearchResult, error) {
	return []yt.SearchResult{{VideoID: "v1", Title: "Hit"}}, nil
}
func (f *fakeService) GetVideo(ctx context.Context, videoID string) (yt.Video, error) {
	return yt.Video{ID: videoID, Title: "A Video"}, nil
}

type fakeTranscripts struct{}

func (fakeTranscripts) Fetch(ctx context.Context, videoID, lang string, withTimestamps bool) (string, error) {
	return "hello transcript in " + lang, nil
}

// session spins up the server with the given provider and connects an
// in-memory MCP client to it.
func session(t *testing.T, provider Provider) *mcp.ClientSession {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "youtube-mcp-test", Version: "0.0.0"}, nil)
	Register(server, provider, fakeTranscripts{})
	st, ct := mcp.NewInMemoryTransports()
	if _, err := server.Connect(context.Background(), st, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	cs, err := client.Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func okProvider(svc Service) Provider {
	return func(ctx context.Context) (Service, error) { return svc, nil }
}

func TestAllTenToolsRegistered(t *testing.T) {
	cs := session(t, okProvider(&fakeService{}))
	resp, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"add_video_to_playlist", "create_playlist", "delete_playlist",
		"get_transcript", "get_video", "list_playlist_items",
		"list_playlists", "remove_video_from_playlist",
		"search_videos", "update_playlist",
	}
	var got []string
	for _, tool := range resp.Tools {
		got = append(got, tool.Name)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d tools %v, want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("tool[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestListPlaylistsTool(t *testing.T) {
	svc := &fakeService{playlists: []yt.Playlist{{ID: "PL1", Title: "Guitar"}}}
	cs := session(t, okProvider(svc))
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "list_playlists", Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("tool errored: %v", res.Content)
	}
	text := fmt.Sprintf("%v", res.StructuredContent)
	if !strings.Contains(text, "PL1") || !strings.Contains(text, "Guitar") {
		t.Errorf("structured content missing playlist: %v", res.StructuredContent)
	}
}

func TestRemoveVideoToolReportsCount(t *testing.T) {
	cs := session(t, okProvider(&fakeService{removed: 2}))
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "remove_video_from_playlist",
		Arguments: map[string]any{"playlist_id": "PL1", "video_id": "v1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("tool errored: %v", res.Content)
	}
	if !strings.Contains(fmt.Sprintf("%v", res.StructuredContent), "2") {
		t.Errorf("removed count missing: %v", res.StructuredContent)
	}
}

func TestGetTranscriptToolDefaultsEnglish(t *testing.T) {
	cs := session(t, okProvider(&fakeService{}))
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "get_transcript", Arguments: map[string]any{"video_id": "v1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fmt.Sprintf("%v", res.StructuredContent), "hello transcript in en") {
		t.Errorf("transcript not returned or wrong default language: %v", res.StructuredContent)
	}
}

func TestUnauthenticatedProviderReturnsToolError(t *testing.T) {
	failing := func(ctx context.Context) (Service, error) {
		return nil, fmt.Errorf("not authenticated: run `youtube-mcp auth` to sign in")
	}
	cs := session(t, failing)
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "list_playlists", Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatal(err) // must be a tool error, not a protocol error
	}
	if !res.IsError {
		t.Fatal("expected IsError result when unauthenticated")
	}
	if !strings.Contains(fmt.Sprintf("%v", res.Content), "youtube-mcp auth") {
		t.Errorf("error content not instructive: %v", res.Content)
	}
}
```

- [x] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tools/`
Expected: FAIL — `undefined: Service`, `Provider`, `Register`

- [x] **Step 3: Write tools.go**

```go
// Package tools defines the MCP tool surface. Handlers depend on narrow
// interfaces so they can be tested with fakes and so the server can start
// before the user has authenticated.
package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"youtube-mcp/internal/yt"
)

// Service is everything the handlers need from the YouTube client.
// *yt.Client satisfies it.
type Service interface {
	ListPlaylists(ctx context.Context, maxResults int64) ([]yt.Playlist, error)
	CreatePlaylist(ctx context.Context, title, description, privacy string) (yt.Playlist, error)
	UpdatePlaylist(ctx context.Context, id string, title, description, privacy *string) (yt.Playlist, error)
	DeletePlaylist(ctx context.Context, id string) error
	ListPlaylistItems(ctx context.Context, playlistID string, maxResults int64, pageToken string) ([]yt.PlaylistItem, string, error)
	AddVideo(ctx context.Context, playlistID, videoID string, position *int64) (yt.PlaylistItem, error)
	RemoveVideo(ctx context.Context, playlistID, videoID string) (int, error)
	SearchVideos(ctx context.Context, query string, maxResults int64) ([]yt.SearchResult, error)
	GetVideo(ctx context.Context, videoID string) (yt.Video, error)
}

// Provider yields the Service lazily so authentication errors surface as
// tool errors instead of preventing server startup.
type Provider func(ctx context.Context) (Service, error)

type TranscriptFetcher interface {
	Fetch(ctx context.Context, videoID, lang string, withTimestamps bool) (string, error)
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
	Position   *int64 `json:"position,omitempty" jsonschema:"0-based position in the playlist (omit to append at the end)"`
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
	Language       string `json:"language,omitempty" jsonschema:"language code such as en or de (default en)"`
	WithTimestamps bool   `json:"with_timestamps,omitempty" jsonschema:"prefix each line with a [mm:ss] timestamp"`
}
type TranscriptOutput struct {
	Transcript string `json:"transcript"`
}

// Register adds all ten tools to the server.
func Register(server *mcp.Server, provider Provider, transcripts TranscriptFetcher) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_playlists",
		Description: "List the authenticated user's YouTube playlists.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in ListPlaylistsInput) (*mcp.CallToolResult, ListPlaylistsOutput, error) {
		svc, err := provider(ctx)
		if err != nil {
			return nil, ListPlaylistsOutput{}, err
		}
		pls, err := svc.ListPlaylists(ctx, in.MaxResults)
		if err != nil {
			return nil, ListPlaylistsOutput{}, err
		}
		return nil, ListPlaylistsOutput{Playlists: pls}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_playlist",
		Description: "Create a new YouTube playlist (private unless specified otherwise).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in CreatePlaylistInput) (*mcp.CallToolResult, PlaylistOutput, error) {
		svc, err := provider(ctx)
		if err != nil {
			return nil, PlaylistOutput{}, err
		}
		pl, err := svc.CreatePlaylist(ctx, in.Title, in.Description, in.PrivacyStatus)
		if err != nil {
			return nil, PlaylistOutput{}, err
		}
		return nil, PlaylistOutput{Playlist: pl}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "update_playlist",
		Description: "Update a playlist's title, description, or privacy. Omitted fields keep their current values.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in UpdatePlaylistInput) (*mcp.CallToolResult, PlaylistOutput, error) {
		svc, err := provider(ctx)
		if err != nil {
			return nil, PlaylistOutput{}, err
		}
		pl, err := svc.UpdatePlaylist(ctx, in.PlaylistID, in.Title, in.Description, in.PrivacyStatus)
		if err != nil {
			return nil, PlaylistOutput{}, err
		}
		return nil, PlaylistOutput{Playlist: pl}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_playlist",
		Description: "PERMANENTLY delete a playlist and all its entries. This cannot be undone.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in DeletePlaylistInput) (*mcp.CallToolResult, DeletePlaylistOutput, error) {
		svc, err := provider(ctx)
		if err != nil {
			return nil, DeletePlaylistOutput{}, err
		}
		if err := svc.DeletePlaylist(ctx, in.PlaylistID); err != nil {
			return nil, DeletePlaylistOutput{}, err
		}
		return nil, DeletePlaylistOutput{Deleted: in.PlaylistID}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_playlist_items",
		Description: "List the videos in a playlist, including the playlist_item_id needed for removals.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in ListItemsInput) (*mcp.CallToolResult, ListItemsOutput, error) {
		svc, err := provider(ctx)
		if err != nil {
			return nil, ListItemsOutput{}, err
		}
		items, next, err := svc.ListPlaylistItems(ctx, in.PlaylistID, in.MaxResults, in.PageToken)
		if err != nil {
			return nil, ListItemsOutput{}, err
		}
		return nil, ListItemsOutput{Items: items, NextPageToken: next}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "add_video_to_playlist",
		Description: "Add a video to a playlist, appending unless a position is given.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in AddVideoInput) (*mcp.CallToolResult, ItemOutput, error) {
		svc, err := provider(ctx)
		if err != nil {
			return nil, ItemOutput{}, err
		}
		item, err := svc.AddVideo(ctx, in.PlaylistID, in.VideoID, in.Position)
		if err != nil {
			return nil, ItemOutput{}, err
		}
		return nil, ItemOutput{Item: item}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "remove_video_from_playlist",
		Description: "Remove every occurrence of a video from a playlist; reports how many entries were removed.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in RemoveVideoInput) (*mcp.CallToolResult, RemoveVideoOutput, error) {
		svc, err := provider(ctx)
		if err != nil {
			return nil, RemoveVideoOutput{}, err
		}
		n, err := svc.RemoveVideo(ctx, in.PlaylistID, in.VideoID)
		if err != nil {
			return nil, RemoveVideoOutput{}, err
		}
		return nil, RemoveVideoOutput{Removed: n}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_videos",
		Description: "Search YouTube for videos. Costs 100 API quota units per call (daily free quota is 10,000).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in SearchInput) (*mcp.CallToolResult, SearchOutput, error) {
		svc, err := provider(ctx)
		if err != nil {
			return nil, SearchOutput{}, err
		}
		results, err := svc.SearchVideos(ctx, in.Query, in.MaxResults)
		if err != nil {
			return nil, SearchOutput{}, err
		}
		return nil, SearchOutput{Results: results}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_video",
		Description: "Get a video's title, channel, duration, view/like counts, and description.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in GetVideoInput) (*mcp.CallToolResult, GetVideoOutput, error) {
		svc, err := provider(ctx)
		if err != nil {
			return nil, GetVideoOutput{}, err
		}
		v, err := svc.GetVideo(ctx, in.VideoID)
		if err != nil {
			return nil, GetVideoOutput{}, err
		}
		return nil, GetVideoOutput{Video: v}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_transcript",
		Description: "Fetch the transcript of any public YouTube video. Needs no authentication.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, in TranscriptInput) (*mcp.CallToolResult, TranscriptOutput, error) {
		lang := in.Language
		if lang == "" {
			lang = "en"
		}
		text, err := transcripts.Fetch(ctx, in.VideoID, lang, in.WithTimestamps)
		if err != nil {
			return nil, TranscriptOutput{}, err
		}
		return nil, TranscriptOutput{Transcript: text}, nil
	})
}
```

- [x] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tools/`
Expected: `ok  youtube-mcp/internal/tools`

Note: `TestAllTenToolsRegistered` expects alphabetical order. If the SDK preserves registration order instead, sort `got` before comparing — the assertion that matters is the exact set of 10 names.

- [x] **Step 5: Run the full suite**

Run: `go test ./...`
Expected: all packages `ok`

- [x] **Step 6: Commit**

```bash
git add internal/tools/
git commit -m "feat: register all ten MCP tools against service interfaces"
```

---

### Task 11: Wire up main.go

**Files:**
- Modify: `main.go` (replace the Task 1 placeholder entirely)

- [x] **Step 1: Write the real main.go**

```go
// youtube-mcp is an MCP server for managing YouTube playlists and fetching
// video transcripts. Subcommands: serve (default, stdio MCP) and auth
// (one-time browser sign-in).
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"youtube-mcp/internal/auth"
	"youtube-mcp/internal/tools"
	"youtube-mcp/internal/transcript"
	"youtube-mcp/internal/yt"
)

const version = "0.1.0"

func main() {
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	ctx := context.Background()
	switch cmd {
	case "serve":
		if err := serve(ctx); err != nil {
			log.Fatal(err)
		}
	case "auth":
		if err := auth.RunAuthFlow(ctx); err != nil {
			log.Fatal(err)
		}
	default:
		fmt.Fprintln(os.Stderr, "usage: youtube-mcp [serve|auth]")
		os.Exit(2)
	}
}

func serve(ctx context.Context) error {
	server := mcp.NewServer(&mcp.Implementation{Name: "youtube-mcp", Version: version}, nil)
	tools.Register(server, newProvider(), transcript.New())
	return server.Run(ctx, &mcp.StdioTransport{})
}

// newProvider builds the YouTube client on first use and caches it on
// success, so the server starts fine before authentication and keeps
// retrying until `youtube-mcp auth` has been run. The client is built on
// context.Background(), not the request context: the oauth2 transport keeps
// using its construction context for token refreshes long after the first
// request ends.
func newProvider() tools.Provider {
	var (
		mu  sync.Mutex
		svc tools.Service
	)
	return func(ctx context.Context) (tools.Service, error) {
		mu.Lock()
		defer mu.Unlock()
		if svc != nil {
			return svc, nil
		}
		hc, err := auth.HTTPClient(context.Background())
		if err != nil {
			return nil, err
		}
		c, err := yt.New(context.Background(), hc)
		if err != nil {
			return nil, err
		}
		svc = c
		return svc, nil
	}
}
```

- [x] **Step 2: Build and vet**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: clean build, all tests `ok`

- [x] **Step 3: Verify the stdio handshake**

```bash
go build -o youtube-mcp .
{ printf '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"probe","version":"0"}}}\n'; sleep 1; } | ./youtube-mcp serve
```

Expected: one JSON-RPC response line on stdout containing `"name":"youtube-mcp"`. The process exits when stdin closes.

- [x] **Step 4: Commit**

```bash
git add main.go
git commit -m "feat: wire serve and auth subcommands with lazy authenticated client"
```

---

### Task 12: README and live smoke test

**Files:**
- Create: `README.md`
- Create: `smoke/smoke_test.go`

- [x] **Step 1: Write README.md**

````markdown
# youtube-mcp

A Go MCP server for managing YouTube playlists and fetching video transcripts
from Claude.

**Tools:** `list_playlists`, `create_playlist`, `update_playlist`,
`delete_playlist`, `list_playlist_items`, `add_video_to_playlist`,
`remove_video_from_playlist`, `search_videos`, `get_video`, `get_transcript`.

Playlist operations use the official YouTube Data API v3 with OAuth.
Transcripts use YouTube's public caption endpoint and need no authentication.

**Not included:** Watch Later. The official API has had no access to the `WL`
playlist since 2016; nothing here can read or clear it.

## Setup

### 1. Google Cloud project (one time, ~5 minutes)

1. Go to <https://console.cloud.google.com/> and create a project
   (e.g. `youtube-mcp`).
2. **APIs & Services → Library** → search "YouTube Data API v3" → **Enable**.
3. **APIs & Services → OAuth consent screen** → External → fill in the app
   name and your email → add yourself as a test user.
4. **APIs & Services → Credentials → Create Credentials → OAuth client ID** →
   Application type **Desktop app**.
5. Download the client JSON and save it as
   `~/.config/youtube-mcp/credentials.json`.

### 2. Build and authenticate

```bash
go build -o youtube-mcp .
./youtube-mcp auth   # opens your browser; approve access
```

The token lands in `~/.config/youtube-mcp/token.json` and refreshes itself.

### 3. Register with Claude Code

```bash
claude mcp add youtube -- /path/to/youtube-mcp
```

## Quota

The Data API's free quota is 10,000 units/day: reads cost 1, playlist
mutations 50, and each `search_videos` call 100. Transcripts cost nothing.
Quota exhaustion comes back as a clear tool error; it resets at midnight
Pacific.

## Caveats

- The transcript endpoint is unofficial; if YouTube changes it, only
  `internal/transcript` needs fixing.
- Age-restricted or region-locked videos may refuse transcript fetches.
````

- [x] **Step 2: Write the smoke test** (build-tag gated; touches the real API with your real account)

```go
//go:build smoke

// Package smoke exercises the live YouTube API end to end. Run manually:
//
//	go test -tags smoke ./smoke -v
//
// Requires ~/.config/youtube-mcp/{credentials,token}.json (run
// `youtube-mcp auth` first). Creates and deletes a throwaway playlist.
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
	ctx := context.Background()
	hc, err := auth.HTTPClient(ctx)
	if err != nil {
		t.Skipf("not authenticated: %v", err)
	}
	c, err := yt.New(ctx, hc)
	if err != nil {
		t.Fatal(err)
	}

	name := fmt.Sprintf("smoke-test-%d", time.Now().Unix())
	pl, err := c.CreatePlaylist(ctx, name, "temporary smoke-test playlist", "private")
	if err != nil {
		t.Fatal(err)
	}
	// Always clean up, even on failure part-way through.
	defer func() {
		if err := c.DeletePlaylist(ctx, pl.ID); err != nil {
			t.Errorf("cleanup failed, delete playlist %s manually: %v", pl.ID, err)
		}
	}()

	// "Me at the zoo" — the first YouTube video; stable and captioned.
	const video = "jNQXAC9IVRw"
	if _, err := c.AddVideo(ctx, pl.ID, video, nil); err != nil {
		t.Fatal(err)
	}
	items, _, err := c.ListPlaylistItems(ctx, pl.ID, 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].VideoID != video {
		t.Fatalf("playlist contents wrong: %+v", items)
	}
	n, err := c.RemoveVideo(ctx, pl.ID, video)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("removed %d, want 1", n)
	}
}

func TestLiveTranscript(t *testing.T) {
	text, err := transcript.New().Fetch(context.Background(), "jNQXAC9IVRw", "en", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(text), "elephant") {
		t.Errorf("transcript doesn't mention elephants?! got: %q", text)
	}
}
```

- [x] **Step 3: Verify the smoke package compiles but is excluded normally**

Run: `go vet -tags smoke ./smoke && go test ./...`
Expected: vet clean; the normal test run does NOT include the smoke package.

- [x] **Step 4: Commit**

```bash
git add README.md smoke/
git commit -m "docs: setup README and tag-gated live smoke test"
```

---

## Definition of Done

- `go build ./... && go vet ./... && go test ./...` all clean.
- Manual: `./youtube-mcp auth` completes in a browser; `go test -tags smoke ./smoke -v` passes against the live API.
- Registered in Claude Code via `claude mcp add youtube -- /path/to/youtube-mcp` and tools respond.
