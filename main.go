// youtube-mcp is an MCP server for managing YouTube playlists and fetching
// video transcripts. Its auth subcommand performs one-time browser sign-in;
// serve (the default) runs the stdio MCP server; `serve --http <addr>` serves
// Streamable HTTP instead, behind a Cloudflare Access JWT check.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime/debug"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"youtube-mcp/internal/accessauth"
	"youtube-mcp/internal/auth"
	"youtube-mcp/internal/tools"
	"youtube-mcp/internal/transcript"
	"youtube-mcp/internal/yt"
)

// version carries the release number reported to MCP clients in the initialize
// handshake. Release builds set it from the git tag via -ldflags
// "-X main.version=...", which only works on a var, so this must not become a
// const. It stays empty everywhere else; resolveVersion supplies the fallback.
var version = ""

// devVersion prefixes the identifier reported by any build that was not stamped
// with a release tag.
const devVersion = "dev"

// resolveVersion reports the injected release version when there is one, and
// otherwise derives an identifier from the VCS stamp that Go embeds in any
// binary built inside the repository.
//
// A hardcoded default would be worse than useless here: every local build would
// claim the same release number no matter which commit produced it, so a client
// showing that number tells you nothing about what is actually running.
func resolveVersion(injected string, readBuildInfo func() (*debug.BuildInfo, bool)) string {
	if injected != "" {
		return injected
	}
	info, ok := readBuildInfo()
	if !ok {
		return devVersion
	}

	var revision string
	var modified bool
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if revision == "" { // built with -buildvcs=false, or outside a repository
		return devVersion
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}
	if modified {
		return devVersion + "-" + revision + "-dirty"
	}
	return devVersion + "-" + revision
}

func main() {
	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}

	ctx := context.Background()
	switch command {
	case "serve":
		flags := flag.NewFlagSet("serve", flag.ExitOnError)
		httpAddr := flags.String("http", "", "listen address for Streamable HTTP, e.g. :8080 (default: stdio)")
		noAccessAuth := flags.Bool("no-access-auth", false, "serve HTTP without Cloudflare Access JWT checks (local development only)")
		var args []string
		if len(os.Args) > 2 {
			args = os.Args[2:]
		}
		flags.Parse(args)
		if err := serve(ctx, *httpAddr, *noAccessAuth); err != nil {
			log.Fatal(err)
		}
	case "auth":
		if err := auth.RunAuthFlow(ctx); err != nil {
			log.Fatal(err)
		}
	default:
		fmt.Fprintln(os.Stderr, "usage: youtube-mcp [serve [--http addr [--no-access-auth]]|auth]")
		os.Exit(2)
	}
}

func serve(ctx context.Context, httpAddr string, noAccessAuth bool) error {
	server := newServer()
	if httpAddr == "" {
		return server.Run(ctx, &mcp.StdioTransport{})
	}
	gate, err := accessGate(ctx, os.Getenv, noAccessAuth)
	if err != nil {
		return err
	}
	log.Printf("listening on %s (MCP endpoint: /mcp)", httpAddr)
	return http.ListenAndServe(httpAddr, newHTTPHandler(server, gate))
}

// accessGate builds the Cloudflare Access JWT check for HTTP mode from
// ACCESS_TEAM_DOMAIN and ACCESS_AUD. It fails closed: missing config is an
// error, not an open server. Only an explicit --no-access-auth returns no gate.
func accessGate(ctx context.Context, getenv func(string) string, disabled bool) (func(http.Handler) http.Handler, error) {
	if disabled {
		log.Print("WARNING: --no-access-auth set; /mcp accepts unauthenticated requests")
		return nil, nil
	}
	cfg := accessauth.Config{
		TeamDomain: getenv("ACCESS_TEAM_DOMAIN"),
		AUD:        getenv("ACCESS_AUD"),
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("%w (set ACCESS_TEAM_DOMAIN and ACCESS_AUD, or pass --no-access-auth for local development)", err)
	}
	return accessauth.Middleware(ctx, cfg), nil
}

// newServer builds the MCP server exactly as stdio mode always has; both
// transports share one instance (the provider cache is mutex-guarded).
func newServer() *mcp.Server {
	reported := resolveVersion(version, debug.ReadBuildInfo)
	server := mcp.NewServer(&mcp.Implementation{Name: "youtube-mcp", Version: reported}, nil)
	tools.Register(server, newProvider(buildService, auth.TokenFingerprint), transcript.New())
	return server
}

// newHTTPHandler mounts the Streamable HTTP handler at /mcp, behind gate when
// one is given.
func newHTTPHandler(server *mcp.Server, gate func(http.Handler) http.Handler) http.Handler {
	var handler http.Handler = mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	if gate != nil {
		handler = gate(handler)
	}
	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)
	return mux
}

// buildService constructs the authenticated YouTube client.
//
// Construction deliberately uses a background context. oauth2 retains its
// construction context for future token refreshes, which must outlive the first
// MCP request that happens to initialize the client.
func buildService() (tools.Service, error) {
	client, err := auth.HTTPClient(context.Background())
	if err != nil {
		return nil, err
	}
	youtube, err := yt.New(context.Background(), client)
	if err != nil {
		return nil, err
	}
	return youtube, nil
}

// newProvider caches the authenticated service, keyed on the fingerprint of the
// token that produced it, and rebuilds whenever that fingerprint changes.
//
// Failed builds are never cached, so tools begin working once the user runs
// `youtube-mcp auth`. Rekeying on the fingerprint covers the other direction: a
// client built from a token that is later revoked — or whose refresh token
// expires, which happens weekly while the OAuth app is in testing mode — holds
// that dead token in memory. Re-running `youtube-mcp auth` rewrites the token
// file, and the next tool call picks it up instead of demanding a restart.
func newProvider(build func() (tools.Service, error), fingerprint func() string) tools.Provider {
	var (
		mu      sync.Mutex
		service tools.Service
		builtAt string
	)
	return func(context.Context) (tools.Service, error) {
		mu.Lock()
		defer mu.Unlock()

		current := fingerprint()
		if service != nil && current == builtAt {
			return service, nil
		}
		built, err := build()
		if err != nil {
			return nil, err
		}
		service, builtAt = built, current
		return service, nil
	}
}
