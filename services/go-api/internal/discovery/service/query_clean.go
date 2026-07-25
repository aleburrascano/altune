package service

import (
	"regexp"
	"strings"
)

var whitespaceRe = regexp.MustCompile(`\s+`)

var trailingFeatRe = regexp.MustCompile(`(?i)\s+(?:feat|ft|featuring)\.?$`)

var noisePatterns = []string{
	"official music video", "official video", "music video",
	"lyric video", "lyrics", "audio",
	"hq", "hd", "4k", "1080p", "720p",
	"full album", "visualizer", "visualiser", "topic",
}

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
