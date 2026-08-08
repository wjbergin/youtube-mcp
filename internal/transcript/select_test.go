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
