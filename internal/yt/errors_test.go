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
