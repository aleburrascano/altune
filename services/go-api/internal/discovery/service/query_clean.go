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

// noiseRes are the noise phrases precompiled as case-insensitive, word-bounded
// regexes, removed with ReplaceAll so every occurrence goes — and matched on the
// ORIGINAL text, never a lowercased copy: lowercasing can change byte offsets
// ("İ" → "i̇"), so index-into-original slicing mangled such queries.
var noiseRes = compileNoisePatterns(noisePatterns)

func compileNoisePatterns(patterns []string) []*regexp.Regexp {
	res := make([]*regexp.Regexp, len(patterns))
	for i, p := range patterns {
		res[i] = regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(p) + `\b`)
	}
	return res
}

func CleanQuery(raw string) string {
	cleaned := raw
	for _, re := range noiseRes {
		cleaned = re.ReplaceAllString(cleaned, " ")
	}
	cleaned = whitespaceRe.ReplaceAllString(cleaned, " ")
	cleaned = strings.TrimSpace(cleaned)
	cleaned = strings.TrimSpace(trailingFeatRe.ReplaceAllString(cleaned, ""))
	if cleaned == "" {
		return raw
	}
	return cleaned
}
