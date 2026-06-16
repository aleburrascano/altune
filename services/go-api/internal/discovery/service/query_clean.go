package service

import (
	"regexp"
	"strings"
)

var queryNoiseRe = regexp.MustCompile(
	`(?i)\b(official\s*(music\s*)?video|lyric(?:s|\s*video)?|audio|hq|hd|4k|1080p|720p|full\s*album|visuali[sz]er|topic)\b`,
)

func CleanQuery(raw string) string {
	cleaned := queryNoiseRe.ReplaceAllString(raw, " ")
	cleaned = whitespaceRe.ReplaceAllString(cleaned, " ")
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return raw
	}
	return cleaned
}
