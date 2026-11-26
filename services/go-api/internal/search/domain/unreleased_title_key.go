package domain

import (
	"altune/go-api/internal/shared/textnorm"
	"altune/go-api/internal/shared/titlemarkers"
	"regexp"
	"strings"
)

var starRunRe = regexp.MustCompile(`\*+`)

var unreleasedMarkerRe = regexp.MustCompile(`(?i)[(\[]\s*unreleased\s*[)\]]`)

func TitleKey(title string) string {
	return TitleKeyWithStar(title, "")
}

func TitleKeyWithStar(title, star string) string {
	cleaned := starRunRe.ReplaceAllString(textnorm.Transliterate(unreleasedMarkerRe.ReplaceAllString(title, " ")), star)
	tokens := titlemarkers.Tokens(titlemarkers.WithoutFeatureCredits(cleaned))
	core := strings.Join(titlemarkers.WithoutVersionPhrases(tokens), " ")
	labels := strings.Join(titlemarkers.RecordingLabels(cleaned), ",")
	return core + "|" + labels
}
