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

func TestSelectTrackPrefersExactLanguageTag(t *testing.T) {
	cases := []struct {
		name   string
		tracks []captionTrack
		lang   string
		want   string
	}{
		{
			name:   "traditional vs simplified chinese",
			tracks: []captionTrack{{BaseURL: "http://x/hant", LanguageCode: "zh-Hant"}, {BaseURL: "http://x/hans", LanguageCode: "zh-Hans"}},
			lang:   "zh-Hans",
			want:   "http://x/hans",
		},
		{
			name:   "european vs brazilian portuguese",
			tracks: []captionTrack{{BaseURL: "http://x/pt", LanguageCode: "pt-PT"}, {BaseURL: "http://x/br", LanguageCode: "pt-BR"}},
			lang:   "pt-BR",
			want:   "http://x/br",
		},
		{
			name:   "exact match is case-insensitive",
			tracks: []captionTrack{{BaseURL: "http://x/pt", LanguageCode: "pt-PT"}, {BaseURL: "http://x/br", LanguageCode: "pt-br"}},
			lang:   "pt-BR",
			want:   "http://x/br",
		},
		{
			name:   "human exact beats asr exact",
			tracks: []captionTrack{{BaseURL: "http://x/asr", LanguageCode: "pt-BR", Kind: "asr"}, {BaseURL: "http://x/manual", LanguageCode: "pt-BR"}},
			lang:   "pt-BR",
			want:   "http://x/manual",
		},
		{
			name:   "exact asr beats human of another region",
			tracks: []captionTrack{{BaseURL: "http://x/pt", LanguageCode: "pt-PT"}, {BaseURL: "http://x/br", LanguageCode: "pt-BR", Kind: "asr"}},
			lang:   "pt-BR",
			want:   "http://x/br",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := selectTrack(tc.tracks, tc.lang)
			if err != nil {
				t.Fatal(err)
			}
			if got.BaseURL != tc.want {
				t.Errorf("picked %q, want %q", got.BaseURL, tc.want)
			}
		})
	}
}

func TestSelectTrackFallsBackToBaseLanguage(t *testing.T) {
	// No exact "pt-BR" track exists, so the base-language match still applies.
	tracks := []captionTrack{{BaseURL: "http://x/pt", LanguageCode: "pt-PT"}}
	got, err := selectTrack(tracks, "pt-BR")
	if err != nil {
		t.Fatal(err)
	}
	if got.BaseURL != "http://x/pt" {
		t.Errorf("picked %q, want the pt-PT track", got.BaseURL)
	}
}
