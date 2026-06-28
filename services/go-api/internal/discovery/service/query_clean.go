package service

import (
	"regexp"
	"strings"
)

var whitespaceRe = regexp.MustCompile(`\s+`)

// trailingFeatRe strips a dangling "feat"/"ft"/"featuring" at the END of a query
// — mid-typing residue with no featured artist after it. Providers expand such a
// query into phantom "Artist feat. X & Y" composite-artist rows that flood the
// slate so the bare artist never surfaces (verified via discoverytrace on
// "Calvin Harris feat"). A *mid-query* "feat" is left intact — it is followed by
// a real featured artist the user wants ("Drake feat Rihanna").
var trailingFeatRe = regexp.MustCompile(`(?i)\s+(?:feat|ft|featuring)\.?$`)

var noisePatterns = []string{
	"official music video", "official video", "music video",
	"lyric video", "lyrics", "audio",
	"hq", "hd", "4k", "1080p", "720p",
	"full album", "visualizer", "visualiser", "topic",
}

func CleanQuery(raw string) string {
	cleaned := raw
	for _, pattern := range noisePatterns {
		cleaned = removeNoisePhrase(cleaned, pattern)
	}
	cleaned = whitespaceRe.ReplaceAllString(cleaned, " ")
	cleaned = strings.TrimSpace(cleaned)
	cleaned = strings.TrimSpace(trailingFeatRe.ReplaceAllString(cleaned, ""))
	if cleaned == "" {
		return raw
	}
	return cleaned
}

func removeNoisePhrase(text, phrase string) string {
	lower := strings.ToLower(text)
	idx := strings.Index(lower, phrase)
	if idx < 0 {
		return text
	}
	before := idx > 0 && isWordBoundary(text[idx-1])
	end := idx + len(phrase)
	after := end >= len(text) || isWordBoundary(text[end])
	if (idx == 0 || before) && after {
		return text[:idx] + " " + text[end:]
	}
	return text
}

func isWordBoundary(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '(' || b == ')' || b == '[' || b == ']'
}
