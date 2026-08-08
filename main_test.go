package main

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"youtube-mcp/internal/tools"
)

// stubService stands in for an authenticated client. The embedded nil
// interface satisfies tools.Service; the provider never calls a method on it.
type stubService struct{ tools.Service }

func TestProviderCachesSuccessfulBuild(t *testing.T) {
	builds := 0
	service := &stubService{}
	provider := newProvider(
		func() (tools.Service, error) {
			builds++
			return service, nil
		},
		func() string { return "token-v1" },
	)

	for call := 1; call <= 3; call++ {
		got, err := provider(context.Background())
		if err != nil {
			t.Fatalf("call %d: %v", call, err)
		}
		if got != service {
			t.Errorf("call %d returned a different service", call)
		}
	}
	if builds != 1 {
		t.Errorf("built the client %d times, want 1", builds)
	}
}

// A failed build must not be cached, so tools start working after the user runs
// `youtube-mcp auth` without restarting the server.
func TestProviderRetriesUntilAuthenticated(t *testing.T) {
	builds := 0
	authenticated := false
	service := &stubService{}
	provider := newProvider(
		func() (tools.Service, error) {
			builds++
			if !authenticated {
				return nil, fmt.Errorf("not authenticated: run `youtube-mcp auth` to sign in")
			}
			return service, nil
		},
		func() string {
			if authenticated {
				return "token-v1"
			}
			return ""
		},
	)

	if _, err := provider(context.Background()); err == nil {
		t.Fatal("expected an authentication error before sign-in")
	}

	authenticated = true
	got, err := provider(context.Background())
	if err != nil {
		t.Fatalf("provider still failing after sign-in: %v", err)
	}
	if got != service {
		t.Error("provider did not return the service built after sign-in")
	}
	if builds != 2 {
		t.Errorf("built the client %d times, want 2", builds)
	}
}

// A revoked or expired token only surfaces when an API call fails, so the user
// re-runs `youtube-mcp auth` against the running server. The cached client
// holds the dead token in memory and must be rebuilt from the new one.
func TestProviderRebuildsAfterReauthentication(t *testing.T) {
	builds := 0
	stale, fresh := &stubService{}, &stubService{}
	token := "token-v1"
	provider := newProvider(
		func() (tools.Service, error) {
			builds++
			if builds == 1 {
				return stale, nil
			}
			return fresh, nil
		},
		func() string { return token },
	)

	got, err := provider(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != stale {
		t.Fatal("first call did not return the initially built service")
	}

	token = "token-v2" // `youtube-mcp auth` rewrote token.json

	got, err = provider(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != fresh {
		t.Error("provider kept the stale client after re-authentication; recovery would need a restart")
	}
}

// The HTTP mode must serve the same MCP server over streamable HTTP. Listing
// tools exercises initialize + tools/list without touching YouTube auth (the
// provider is lazy — nothing builds the client until a tool call).
func TestHTTPHandlerServesMCP(t *testing.T) {
	ts := httptest.NewServer(newHTTPHandler(newServer()))
	defer ts.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.0"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint: ts.URL + "/mcp",
	}, nil)
	if err != nil {
		t.Fatalf("connecting over streamable HTTP: %v", err)
	}
	defer session.Close()

	result, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("listing tools: %v", err)
	}

	names := make(map[string]bool, len(result.Tools))
	for _, tool := range result.Tools {
		names[tool.Name] = true
	}
	for _, want := range []string{"list_playlists", "get_transcript"} {
		if !names[want] {
			t.Errorf("tool %q not exposed over HTTP; got %v", want, result.Tools)
		}
	}
}
