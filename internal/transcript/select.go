package transcript

import (
	"fmt"
	"sort"
	"strings"
)

type captionTrack struct {
	BaseURL      string `json:"baseUrl"`
	LanguageCode string `json:"languageCode"`
	Kind         string `json:"kind"` // "asr" means auto-generated
}

// selectTrack picks the caption track for lang, preferring human-made captions
// over auto-generated ones. Language matching ignores region subtags, so
// lang "en" matches a track coded "en-GB".
func selectTrack(tracks []captionTrack, lang string) (captionTrack, error) {
	if len(tracks) == 0 {
		return captionTrack{}, fmt.Errorf("video has no captions")
	}
	var matches []captionTrack
	for _, tr := range tracks {
		if baseLang(tr.LanguageCode) == baseLang(lang) {
			matches = append(matches, tr)
		}
	}
	if len(matches) == 0 {
		return captionTrack{}, fmt.Errorf("no %q captions for this video; available: %s", lang, availableLanguages(tracks))
	}
	for _, tr := range matches {
		if tr.Kind != "asr" {
			return tr, nil
		}
	}
	return matches[0], nil
}

func baseLang(code string) string {
	return strings.ToLower(strings.SplitN(code, "-", 2)[0])
}

func availableLanguages(tracks []captionTrack) string {
	var langs []string
	for _, tr := range tracks {
		l := tr.LanguageCode
		if tr.Kind == "asr" {
			l += " (auto-generated)"
		}
		langs = append(langs, l)
	}
	sort.Strings(langs)
	return strings.Join(langs, ", ")
}
