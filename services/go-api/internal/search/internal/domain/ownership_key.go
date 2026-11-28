package domain

import (
	"altune/go-api/internal/shared/music"
	"regexp"
	"strings"
)

var (
	bracketGroupRe  = regexp.MustCompile(`\s*[\(\[\{][^\)\]\}]*[\)\]\}]`)
	dashSeparatorRe = regexp.MustCompile(`\s+[-\x{2013}\x{2014}]\s+`)
)

const isrcKeyPrefix = "isrc:"

func OwnershipKey(title, artist string) string {
	return titleKey(title) + "|" + music.NormalizeForMatch(artist)
}

func OwnershipISRCKey(isrc string) string {
	canonical := music.CanonicalISRC(isrc)
	if canonical == "" {
		return ""
	}
	return isrcKeyPrefix + canonical
}

func titleKey(title string) string {
	withoutBrackets := bracketGroupRe.ReplaceAllStringFunc(title, keepUnlessDecoration)
	parts := dashSeparatorRe.Split(withoutBrackets, -1)
	kept := parts[:1]
	for _, suffix := range parts[1:] {
		if !isDecoration(suffix) {
			kept = append(kept, suffix)
		}
	}
	return music.NormalizeForIdentity(strings.Join(kept, " "))
}

func keepUnlessDecoration(segment string) string {
	if isDecoration(segment) {
		return ""
	}
	return segment
}

func isDecoration(segment string) bool {
	words := music.NormalizeForIdentity(strings.Trim(segment, " ()[]{}-\u2013\u2014"))
	return music.IsEditionLabel(words) || music.IsReleaseMarker(words)
}
