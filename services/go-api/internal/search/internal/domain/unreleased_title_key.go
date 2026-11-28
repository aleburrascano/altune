package domain

import (
	"altune/go-api/internal/shared/music"
	"regexp"
	"strings"
)

var starRunRe = regexp.MustCompile(`\*+`)

var unreleasedMarkerRe = regexp.MustCompile(`(?i)[(\[]\s*unreleased\s*[)\]]`)

func TitleKey(title string) string {
	return TitleKeyWithStar(title, "")
}

func TitleKeyWithStar(title, star string) string {
	cleaned := starRunRe.ReplaceAllString(music.Transliterate(unreleasedMarkerRe.ReplaceAllString(title, " ")), star)
	tokens := music.TitleTokens(music.StripFeatureCredits(cleaned))
	core := strings.Join(music.StripVersionPhrases(tokens), " ")
	labels := strings.Join(music.RecordingLabels(cleaned), ",")
	return core + "|" + labels
}
