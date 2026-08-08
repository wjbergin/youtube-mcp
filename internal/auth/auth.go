// Package auth handles the OAuth credential lifecycle: a one-time browser
// consent flow and a cached, automatically refreshed token thereafter.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
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

// Dir locates youtube-mcp's private configuration directory. It is a variable
// so tests can redirect credential access into a temporary directory.
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

// LoadOAuthConfig reads the Google Desktop-app credentials and requests the
// full YouTube scope needed for playlist mutations.
func LoadOAuthConfig() (*oauth2.Config, error) {
	path, err := credentialsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("no OAuth credentials at %s: create a Desktop-app OAuth client in Google Cloud Console and save its credentials.json there (see README)", path)
		}
		return nil, fmt.Errorf("reading OAuth credentials at %s: %w", path, err)
	}
	config, err := google.ConfigFromJSON(data, ytapi.YoutubeScope)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return config, nil
}

// LoadToken reads the cached OAuth token. Missing and corrupt caches return
// actionable errors so MCP tools can tell the user how to recover.
func LoadToken() (*oauth2.Token, error) {
	path, err := tokenPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("not authenticated: run `youtube-mcp auth` to sign in")
		}
		return nil, fmt.Errorf("reading OAuth token at %s: %w", path, err)
	}
	var token oauth2.Token
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, fmt.Errorf("corrupt token at %s: delete it and run `youtube-mcp auth` again", path)
	}
	return &token, nil
}

// TokenFingerprint identifies the cached token's current contents so a
// long-running server can notice that `youtube-mcp auth` has replaced it and
// rebuild its client instead of holding a dead token until restart. It returns
// "" when no readable token exists, which callers treat as "nothing to reuse".
//
// The value derives from the file, not from an in-memory token: refreshed
// access tokens are never written back, so this stays stable during normal
// operation and changes only on re-authentication.
func TokenFingerprint() string {
	path, err := tokenPath()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// SaveToken stores an OAuth token in a private configuration directory.
func SaveToken(token *oauth2.Token) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("securing config directory: %w", err)
	}
	data, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("encoding OAuth token: %w", err)
	}
	path, err := tokenPath()
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("opening token cache: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return fmt.Errorf("securing token cache: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return fmt.Errorf("writing token cache: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("closing token cache: %w", err)
	}
	return nil
}

// HTTPClient returns an authenticated HTTP client whose OAuth transport
// refreshes access tokens automatically.
func HTTPClient(ctx context.Context) (*http.Client, error) {
	config, err := LoadOAuthConfig()
	if err != nil {
		return nil, err
	}
	token, err := LoadToken()
	if err != nil {
		return nil, err
	}
	return config.Client(ctx, token), nil
}

// RunAuthFlow opens Google's consent page, receives its redirect on an
// ephemeral loopback listener, exchanges the code, and caches the token.
func RunAuthFlow(ctx context.Context) error {
	config, err := LoadOAuthConfig()
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("starting OAuth callback listener: %w", err)
	}
	defer listener.Close()
	config.RedirectURL = fmt.Sprintf("http://%s/", listener.Addr())

	state, err := randomState()
	if err != nil {
		return err
	}
	resultCh := make(chan callbackResult, 1)
	server := &http.Server{Handler: callbackHandler(state, resultCh)}
	serveErrCh := make(chan error, 1)
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErrCh <- fmt.Errorf("serving OAuth callback: %w", err)
		}
	}()
	defer server.Close()

	authURL := config.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce)
	fmt.Fprintf(os.Stderr, "Opening browser for authorization...\nIf it doesn't open, visit:\n%s\n", authURL)
	openBrowser(authURL)

	var result callbackResult
	select {
	case result = <-resultCh:
		if result.err != nil {
			return result.err
		}
	case err := <-serveErrCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
	token, err := config.Exchange(ctx, result.code)
	if err != nil {
		return fmt.Errorf("exchanging authorization code: %w", err)
	}
	if err := SaveToken(token); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "Token saved. youtube-mcp is ready to use.")
	return nil
}

type callbackResult struct {
	code string
	err  error
}

func callbackHandler(expectedState string, results chan<- callbackResult) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("state") != expectedState {
			http.Error(w, "invalid OAuth state", http.StatusBadRequest)
			return
		}

		if oauthErr := request.URL.Query().Get("error"); oauthErr != "" {
			result := callbackResult{err: fmt.Errorf("authorization failed: %s", oauthErr)}
			deliverCallbackResult(results, result)
			http.Error(w, result.err.Error(), http.StatusBadRequest)
			return
		}
		code := request.URL.Query().Get("code")
		if code == "" {
			result := callbackResult{err: fmt.Errorf("authorization callback did not include a code")}
			deliverCallbackResult(results, result)
			http.Error(w, result.err.Error(), http.StatusBadRequest)
			return
		}

		if deliverCallbackResult(results, callbackResult{code: code}) {
			fmt.Fprintln(w, "Authorized. You can close this tab.")
			return
		}
		fmt.Fprintln(w, "Authorization was already received. You can close this tab.")
	})
}

func deliverCallbackResult(results chan<- callbackResult, result callbackResult) bool {
	select {
	case results <- result:
		return true
	default:
		return false
	}
}

func randomState() (string, error) {
	var state [32]byte
	if _, err := rand.Read(state[:]); err != nil {
		return "", fmt.Errorf("generating OAuth state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(state[:]), nil
}

func openBrowser(url string) {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", url)
	case "linux":
		command = exec.Command("xdg-open", url)
	default:
		return
	}
	_ = command.Start()
}
