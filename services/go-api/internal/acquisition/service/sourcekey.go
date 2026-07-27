package service

import (
	"regexp"
	"strings"
)

var youtubeVideoIDRe = regexp.MustCompile(
	`(?i)(?:youtube\.com/watch\?(?:[^ ]*&)?v=|youtu\.be/|youtube\.com/embed/|youtube\.com/v/)([A-Za-z0-9_-]{11})`)

func sourceKey(rawURL string) string {
	if m := youtubeVideoIDRe.FindStringSubmatch(rawURL); m != nil {
		return "youtube:" + m[1]
	}

	key := strings.TrimSpace(rawURL)
	if i := strings.IndexByte(key, '?'); i >= 0 {
		key = key[:i]
	}
	key = strings.TrimPrefix(key, "https://")
	key = strings.TrimPrefix(key, "http://")
	key = strings.TrimPrefix(key, "www.")
	key = strings.TrimSuffix(key, "/")
	return strings.ToLower(key)
}

func SourceKeys(rawURLs []string) []string {
	keys := make([]string, 0, len(rawURLs))
	for _, raw := range rawURLs {
		keys = append(keys, sourceKey(raw))
	}
	return mergeSourceKeys(nil, keys...)
}

func mergeSourceKeys(existing []string, add ...string) []string {
	merged := make([]string, 0, len(existing)+len(add))
	seen := make(map[string]bool, len(existing)+len(add))
	for _, key := range append(append([]string{}, existing...), add...) {
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		merged = append(merged, key)
	}
	return merged
}
