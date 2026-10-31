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

func baseKey(title string) string {
	if len(versiontag.RecordingLabels(title)) > 0 {
		return ""
	}
	base, _ := versiontag.BaseTitle(title)
	return titleKey(base)
}
