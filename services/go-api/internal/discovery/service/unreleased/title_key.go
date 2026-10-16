package unreleased

import (
	"altune/go-api/internal/shared/versiontag"
	"regexp"
	"strings"
)

var unreleasedMarkerRe = regexp.MustCompile(`(?i)[(\[]\s*unreleased\s*[)\]]`)

func titleKey(title string) string {
	cleaned := strings.ReplaceAll(unreleasedMarkerRe.ReplaceAllString(title, " "), "*", "")
	tokens := versiontag.Tokens(versiontag.WithoutFeatureCredits(cleaned))
	core := strings.Join(versiontag.WithoutVersionPhrases(tokens), " ")
	labels := strings.Join(versiontag.RecordingLabels(cleaned), ",")
	return core + "|" + labels
}
