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

// selectTrack picks the caption track for lang. An exact (case-insensitive)
// match on the full language tag wins, so lang "pt-BR" selects the Brazilian
// track over "pt-PT"; only when no exact match exists does matching fall back
// to the base language, so lang "en" still matches a track coded "en-GB".
// Within each tier, human-made captions beat auto-generated ones.
func selectTrack(tracks []captionTrack, lang string) (captionTrack, error) {
	if len(tracks) == 0 {
		return captionTrack{}, fmt.Errorf("video has no captions")
	}
	var exact, base []captionTrack
	for _, tr := range tracks {
		switch {
		case strings.EqualFold(tr.LanguageCode, lang):
			exact = append(exact, tr)
		case baseLang(tr.LanguageCode) == baseLang(lang):
			base = append(base, tr)
		}
	}
	for _, tier := range [][]captionTrack{exact, base} {
		if tr, ok := preferHuman(tier); ok {
			return tr, nil
		}
	}
	return captionTrack{}, fmt.Errorf("no %q captions for this video; available: %s", lang, availableLanguages(tracks))
}

// preferHuman returns the first non-ASR track, or the first track of any kind
// if every match is auto-generated. ok is false for an empty tier.
func preferHuman(tracks []captionTrack) (tr captionTrack, ok bool) {
	if len(tracks) == 0 {
		return captionTrack{}, false
	}
	for _, tr := range tracks {
		if tr.Kind != "asr" {
			return tr, true
		}
	}
	return tracks[0], true
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
