# youtube-mcp

A Go MCP server for managing YouTube playlists and fetching video transcripts
from Claude.

Available tools: `list_playlists`, `create_playlist`, `update_playlist`,
`delete_playlist`, `list_playlist_items`, `add_video_to_playlist`,
`remove_video_from_playlist`, `search_videos`, `get_video`, and
`get_transcript`.

Playlist and video operations use the official YouTube Data API v3 with OAuth.
Transcripts use YouTube's public caption endpoint and do not require
authentication.

Watch Later is intentionally not included. The official API has not exposed
the `WL` playlist since 2016, so this server cannot read or clear it.

## Setup

### 1. Create Google credentials

This is a one-time setup and usually takes about five minutes.

1. Open the [Google Cloud Console](https://console.cloud.google.com/) and create
   a project, such as `youtube-mcp`.
2. Go to **APIs & Services → Library**, find **YouTube Data API v3**, and enable
   it.
3. Configure the **OAuth consent screen**. Choose **External**, provide the app
   name and your email, and add yourself as a test user if the app remains in
   testing mode.
4. Go to **APIs & Services → Credentials → Create Credentials → OAuth client
   ID** and choose **Desktop app**.
5. Download the client JSON and save it as:

   ```text
   ~/.config/youtube-mcp/credentials.json
   ```

### 2. Build and authenticate

The project requires Go 1.26 or newer.

```bash
go build -o youtube-mcp .
./youtube-mcp auth
```

The auth command opens a browser for consent. The resulting token is stored at
`~/.config/youtube-mcp/token.json` with private file permissions and refreshes
automatically.

### 3. Register with Claude Code

Use the absolute path to the binary you built:

```bash
claude mcp add youtube -- /absolute/path/to/youtube-mcp
```

Running the binary without a subcommand defaults to its stdio MCP server. You
can also run that mode explicitly with `youtube-mcp serve`.

## Remote (HTTP)

`serve --http <addr>` serves the same MCP server over Streamable HTTP at
`/mcp` instead of stdio. Scope the bind address to a trusted interface:

```bash
./youtube-mcp serve --http <tailscale-ip>:8080
```

There is no authentication on the HTTP endpoint — run it only on a network
you trust (e.g. bound to a Tailscale interface). Register in Claude Code
with:

```bash
claude mcp add --transport http youtube http://<host>:8080/mcp
```

A `Dockerfile` is included; the container runs this mode and expects the
OAuth files mounted read-only at `/home/app/.config/youtube-mcp`.

## Development

Run the local verification suite with:

```bash
go build ./...
go vet ./...
go test ./...
```

A live smoke test is available after authentication. It creates and deletes a
temporary private playlist in the authenticated account and also calls the
unofficial transcript endpoint:

```bash
go test -tags smoke ./smoke -v
```

## Releases

Pushing a `v*` tag builds macOS and Linux binaries (arm64 and amd64 each) and
publishes them as a GitHub release:

```bash
git tag v0.2.0
git push origin v0.2.0
```

The tag is the source of truth for the version: the workflow strips the leading
`v` and injects the rest with `-ldflags "-X main.version=..."`, which is what
MCP clients see in the initialize handshake. Tests must pass before any binary
is built.

Builds without that flag report the commit they were built from instead, taken
from the VCS stamp Go embeds automatically: `dev-368d7fa1b2c3`, with a `-dirty`
suffix when the working tree had uncommitted changes. So the version a client
shows always identifies the binary actually running, whether it came from a
release or from `go build` on your machine.

Each archive carries the binary and this README; `checksums.txt` covers all of
them and is verified with `shasum -a 256 -c checksums.txt`.

## Quota

The Data API's default free quota is 10,000 units per day. Reads generally cost
1 unit, playlist mutations cost 50, and each `search_videos` call costs 100.
Transcript fetching bypasses the Data API and consumes no API quota. Quota
exhaustion is returned as a tool error and resets at midnight Pacific time.

## Caveats

- The transcript endpoint is unofficial. Its implementation is isolated in
  `internal/transcript` so an upstream change remains a one-package fix.
- Age-restricted, private, or region-locked videos may refuse transcript
  fetching.
- Playlist deletion is permanent; its tool description marks it as
  destructive.
