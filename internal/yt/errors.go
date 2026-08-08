// Package yt wraps the generated YouTube Data API client behind a small,
// testable surface. All quota-consuming calls live here.
package yt

import (
	"errors"
	"fmt"
	"slices"
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
		case hasReason(gerr, "quotaExceeded", "dailyLimitExceeded"):
			return fmt.Errorf("YouTube API daily quota exhausted; it resets at midnight Pacific time")
		// Throttling, not an exhausted allowance: the same 403 the daily quota
		// uses, but the fix is a brief wait rather than waiting for the reset.
		case hasReason(gerr, "rateLimitExceeded", "userRateLimitExceeded"):
			return fmt.Errorf("sending requests too quickly to the YouTube API; wait a few seconds and retry")
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

func hasReason(gerr *googleapi.Error, reasons ...string) bool {
	for _, e := range gerr.Errors {
		if slices.Contains(reasons, e.Reason) {
			return true
		}
	}
	return false
}
