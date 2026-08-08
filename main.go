// youtube-mcp is an MCP server for managing YouTube playlists and fetching
// video transcripts. Its auth subcommand performs one-time browser sign-in;
// serve (the default) runs the stdio MCP server.
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
	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}

	ctx := context.Background()
	switch command {
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

// newProvider builds and caches the authenticated API service on first use.
// Failed attempts are not cached, so tools start working after the user runs
// `youtube-mcp auth` without requiring a server restart.
//
// Construction deliberately uses a background context. oauth2 retains its
// construction context for future refreshes, which must outlive the first MCP
// request that happens to initialize the client.
func newProvider() tools.Provider {
	var (
		mu      sync.Mutex
		service tools.Service
	)
	return func(context.Context) (tools.Service, error) {
		mu.Lock()
		defer mu.Unlock()
		if service != nil {
			return service, nil
		}
		client, err := auth.HTTPClient(context.Background())
		if err != nil {
			return nil, err
		}
		youtube, err := yt.New(context.Background(), client)
		if err != nil {
			return nil, err
		}
		service = youtube
		return service, nil
	}
}
