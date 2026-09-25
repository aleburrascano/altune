package service

import "altune/go-api/internal/acquisition/ports"

// sourceKey is ports.SourceKey under the name this package's exclude and log
// call sites already use; the canonicalizer lives in ports so candidate dedupe
// keys on the same identity an exclude does.
func sourceKey(rawURL string) string { return ports.SourceKey(rawURL) }

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
