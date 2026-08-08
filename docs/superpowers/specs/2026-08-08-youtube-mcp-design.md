# YouTube MCP Server — Design

**Date:** 2026-08-08
**Status:** Approved
**Location:** `/Users/bill/work/youtube-mcp`

## Overview

A Go MCP server (stdio transport) that lets Claude manage YouTube playlists and
fetch video transcripts. Playlist operations use the official YouTube Data API v3
with OAuth; transcripts use YouTube's unauthenticated InnerTube caption endpoint
(the same mechanism `youtube-transcript-api` uses).

Inspired by [ia-programming/youtube-mcp](https://github.com/ia-programming/youtube-mcp)
but a fresh design: that project is Python, has no playlist management, and carries
vector-DB/semantic-search machinery we don't need.

## Goals

- Create, update, and delete playlists on the user's channel
- List playlists and their items; add/remove videos
- Search videos and fetch video details via the official API
- Fetch transcripts for arbitrary public videos

## Non-Goals (explicit decisions)

- **No Watch Later access.** The official API removed access to the `WL` playlist
  in 2016. Rather than cookie-based InnerTube hacks or browser automation inside
  the server, Watch Later clearing stays out of scope; Claude-in-Chrome handles it
  interactively when needed.
- **No vector DB / semantic transcript search.** Transcripts return directly to
  the model.
- **No persistence** beyond the OAuth token cache.
- **No HTTP/SSE transport.** Stdio only, launched by Claude Code.

## Stack

| Dependency | Purpose |
|---|---|
| `github.com/modelcontextprotocol/go-sdk` | Official MCP SDK (Anthropic + Google); typed tool handlers, stdio serving |
| `google.golang.org/api/youtube/v3` | Generated YouTube Data API client |
| `golang.org/x/oauth2` | Token exchange, caching, auto-refresh |

The only fully custom network code is the transcript fetcher.

## Layout

```
youtube-mcp/
  main.go               # subcommands: `serve` (default, stdio MCP) and `auth`
  internal/auth/        # credentials.json + token.json loading, first-run flow
  internal/youtube/     # thin wrapper over the generated youtube/v3 client
  internal/transcript/  # InnerTube caption fetcher (no auth)
  docs/
```

Each `internal` package has one job and takes injected dependencies
(`*http.Client`, service interfaces) so it can be tested without the network.

## Authentication

One-time setup, then invisible:

1. User creates a Google Cloud project, enables **YouTube Data API v3**, creates
   an OAuth **Desktop app** client, and saves its JSON to
   `~/.config/youtube-mcp/credentials.json`. The README documents this
   step-by-step.
2. `youtube-mcp auth` opens the browser for consent (scope:
   `https://www.googleapis.com/auth/youtube` — full read/write, required for
   playlist mutations), catches the redirect on a loopback listener at
   `127.0.0.1:<ephemeral>`, and writes `~/.config/youtube-mcp/token.json`.
3. `youtube-mcp serve` loads the cached token and refreshes automatically. A
   missing or revoked token does not kill the server: every tool returns a clear
   "run `youtube-mcp auth`" error instead.

Registration: `claude mcp add youtube -- /path/to/youtube-mcp`.

## Tools

All parameter schemas derived from Go structs by the SDK. `?` = optional.

| Tool | Params | Behavior |
|---|---|---|
| `list_playlists` | `max_results?` (default 50) | User's playlists: id, title, description, privacy, item count |
| `create_playlist` | `title`, `description?`, `privacy_status?` | `private` (default) / `unlisted` / `public`; returns new playlist id + URL |
| `update_playlist` | `playlist_id`, `title?`, `description?`, `privacy_status?` | API requires a full snippet on update, so the wrapper fetches the current snippet and merges only the provided fields |
| `delete_playlist` | `playlist_id` | Irreversible; tool description marks it destructive |
| `list_playlist_items` | `playlist_id`, `max_results?` (default 50), `page_token?` | Video ids, titles, positions, and `playlist_item_id`s; paginated |
| `add_video_to_playlist` | `playlist_id`, `video_id`, `position?` | Appends by default |
| `remove_video_from_playlist` | `playlist_id`, `video_id` | Resolves matching playlist-item ids internally; removes **all** occurrences of the video and reports the count |
| `search_videos` | `query`, `max_results?` (default 10, ≤50) | Official search; returns video id, title, channel, published date |
| `get_video` | `video_id` | Title, channel, duration, view/like counts, description |
| `get_transcript` | `video_id`, `language?` (default `en`), `with_timestamps?` (default false) | Plain text; timestamps as `[m:ss]` / `[h:mm:ss]` prefixes when requested |

### Quota notes

Default free quota is 10,000 units/day: reads cost 1, playlist mutations 50,
`search_videos` 100. Quota exhaustion surfaces as a clear error (see below), not
a crash. Transcripts cost no quota (they bypass the Data API).

## Transcript mechanism

1. POST to InnerTube `player` endpoint with the **ANDROID client context** — no
   user credentials. (Discovered during implementation: WEB-client player calls
   are PO-token-gated and return UNPLAYABLE; the ANDROID client works.)
2. Read `captions.playerCaptionsTracklistRenderer.captionTracks`.
3. Pick the requested language, preferring a human-made track over
   auto-generated (`kind == "asr"`).
4. Fetch the track's `baseUrl` with the `fmt` query param **replaced** by
   `json3` (ANDROID URLs already carry `fmt=srv3`, and YouTube honors the first
   occurrence); flatten events to text.

If the requested language is missing, the error lists the languages that exist
so the model can retry. Known limitation: age-restricted or region-locked videos
may fail; the error says so explicitly. This endpoint is unofficial and could
change — the fetcher is isolated in `internal/transcript` so a breakage is a
one-package fix.

## Error handling

Tool failures return MCP tool errors with actionable text; the server never
exits on a tool error. `internal/youtube` maps `googleapi.Error` to plain
language:

| API condition | Message |
|---|---|
| 403 `quotaExceeded` | Daily quota exhausted; resets midnight Pacific |
| 404 | No playlist/video with that id |
| 401 / `invalid_grant` | Token expired or revoked — run `youtube-mcp auth` |
| Transcript: no tracks | Video has no captions |
| Transcript: wrong language | Not available in *lang*; available: … |
| Transcript: player error | Video unavailable / age-restricted |

## Testing

- **Unit tests** (no network): `internal/transcript` parses recorded json3
  fixtures via `httptest`; `internal/youtube` covers update-merge,
  remove-all-occurrences resolution, and error mapping with a stubbed transport.
- **Handler tests**: MCP tool handlers run against a fake service interface.
- **Manual smoke test**: a script gated behind a real-credentials env var
  exercises the live API end-to-end (create → mutate → delete a throwaway
  playlist, fetch a known transcript). Not part of CI.
