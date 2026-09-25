package providers

import (
	"altune/go-api/internal/discovery/domain"
	"strings"
)

var itunesTypeSuffixes = []string{" - Single", " - EP", " - Album", " - Deluxe", " - Remix"}

func stripITunesTypeSuffix(name string) string {
	lower := strings.ToLower(name)
	if len(lower) != len(name) {
		return name
	}
	for _, suffix := range itunesTypeSuffixes {
		if strings.HasSuffix(lower, strings.ToLower(suffix)) {
			return strings.TrimSpace(name[:len(name)-len(suffix)])
		}
	}
	return name
}

func stripAlbumTypeSuffix(title string) string {
	for _, suffix := range []string{" - Single", " - EP"} {
		if len(title) >= len(suffix) && strings.EqualFold(title[len(title)-len(suffix):], suffix) {
			return strings.TrimSpace(title[:len(title)-len(suffix)])
		}
	}
	return title
}

func iTunesRecordType(collectionName string) domain.RecordType {
	lower := strings.ToLower(collectionName)
	switch {
	case strings.Contains(lower, " - single"):
		return domain.RecordTypeSingle
	case strings.Contains(lower, " - ep"):
		return domain.RecordTypeEP
	default:
		return domain.RecordTypeAlbum
	}
}
