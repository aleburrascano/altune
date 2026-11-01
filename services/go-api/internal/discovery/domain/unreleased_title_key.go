package domain

import (
	"altune/go-api/internal/shared/textnorm"
	"altune/go-api/internal/shared/versiontag"
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
	tokens := versiontag.Tokens(versiontag.WithoutFeatureCredits(cleaned))
	core := strings.Join(versiontag.WithoutVersionPhrases(tokens), " ")
	labels := strings.Join(versiontag.RecordingLabels(cleaned), ",")
	return core + "|" + labels
}
