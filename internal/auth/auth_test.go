package auth

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func withTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	original := Dir
	Dir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { Dir = original })
	return dir
}

func TestTokenRoundTrip(t *testing.T) {
	withTempDir(t)
	token := &oauth2.Token{
		AccessToken:  "at",
		RefreshToken: "rt",
		Expiry:       time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := SaveToken(token); err != nil {
		t.Fatal(err)
	}
	got, err := LoadToken()
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "at" || got.RefreshToken != "rt" || !got.Expiry.Equal(token.Expiry) {
		t.Errorf("got %+v", got)
	}
}

func TestSaveTokenUsesPrivatePermissions(t *testing.T) {
	dir := withTempDir(t)
	if err := SaveToken(&oauth2.Token{AccessToken: "secret"}); err != nil {
		t.Fatal(err)
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Errorf("config directory permissions = %o, want 700", got)
	}
	tokenInfo, err := os.Stat(filepath.Join(dir, "token.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := tokenInfo.Mode().Perm(); got != 0o600 {
		t.Errorf("token permissions = %o, want 600", got)
	}
}

func TestSaveTokenRepairsExistingPermissions(t *testing.T) {
	dir := withTempDir(t)
	path := filepath.Join(dir, "token.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveToken(&oauth2.Token{AccessToken: "secret"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("token permissions = %o, want repaired mode 600", got)
	}
}

func TestLoadTokenMissingTellsUserToAuth(t *testing.T) {
	withTempDir(t)
	_, err := LoadToken()
	if err == nil || !strings.Contains(err.Error(), "youtube-mcp auth") {
		t.Errorf("missing token must instruct re-auth, got: %v", err)
	}
}

func TestLoadTokenCorruptTellsUserToAuth(t *testing.T) {
	dir := withTempDir(t)
	if err := os.WriteFile(filepath.Join(dir, "token.json"), []byte(`not-json`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadToken()
	if err == nil || !strings.Contains(err.Error(), "youtube-mcp auth") {
		t.Errorf("corrupt token must instruct re-auth, got: %v", err)
	}
}

func TestLoadOAuthConfigMissingMentionsCredentials(t *testing.T) {
	withTempDir(t)
	_, err := LoadOAuthConfig()
	if err == nil || !strings.Contains(err.Error(), "credentials.json") {
		t.Errorf("missing credentials must point at setup docs, got: %v", err)
	}
}

func TestRandomState(t *testing.T) {
	first, err := randomState()
	if err != nil {
		t.Fatal(err)
	}
	second, err := randomState()
	if err != nil {
		t.Fatal(err)
	}
	if first == "" || first == second {
		t.Errorf("OAuth states must be non-empty and unique: %q, %q", first, second)
	}
}

func TestCallbackHandlerAcceptsAuthorizationCode(t *testing.T) {
	results := make(chan callbackResult, 1)
	request := httptest.NewRequest(http.MethodGet, "/?state=expected&code=auth-code", nil)
	response := httptest.NewRecorder()
	callbackHandler("expected", results).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	result := <-results
	if result.code != "auth-code" || result.err != nil {
		t.Errorf("callback result = %+v", result)
	}
}

func TestCallbackHandlerReportsDenial(t *testing.T) {
	results := make(chan callbackResult, 1)
	request := httptest.NewRequest(http.MethodGet, "/?state=expected&error=access_denied", nil)
	response := httptest.NewRecorder()
	callbackHandler("expected", results).ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
	result := <-results
	if result.err == nil || !strings.Contains(result.err.Error(), "access_denied") {
		t.Errorf("callback result = %+v", result)
	}
}

func TestCallbackHandlerRejectsInvalidStateWithoutEndingFlow(t *testing.T) {
	results := make(chan callbackResult, 1)
	request := httptest.NewRequest(http.MethodGet, "/?state=wrong&code=auth-code", nil)
	response := httptest.NewRecorder()
	callbackHandler("expected", results).ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
	select {
	case result := <-results:
		t.Fatalf("invalid state ended the auth flow: %+v", result)
	default:
	}
}
