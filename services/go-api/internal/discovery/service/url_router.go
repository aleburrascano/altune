package service

import (
	"regexp"
	"strings"

	"altune/go-api/internal/discovery/domain"
)

var urlPatterns = []struct {
	provider domain.ProviderName
	pattern  *regexp.Regexp
}{
	{domain.ProviderDeezer, regexp.MustCompile(`(?i)deezer\.com/`)},
	{domain.ProviderMusicBrainz, regexp.MustCompile(`(?i)musicbrainz\.org/`)},
	{domain.ProviderSoundCloud, regexp.MustCompile(`(?i)soundcloud\.com/`)},
	{domain.ProviderLastFM, regexp.MustCompile(`(?i)last\.fm/`)},
}

func DetectProvider(url string) (domain.ProviderName, bool) {
	url = strings.TrimSpace(url)
	for _, p := range urlPatterns {
		if p.pattern.MatchString(url) {
			return p.provider, true
		}
	}
	return 0, false
}
