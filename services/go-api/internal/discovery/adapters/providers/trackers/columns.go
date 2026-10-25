package trackers

import (
	"regexp"
	"strings"
	"unicode"
)

const (
	headerScanRows   = 5
	inferSampleRows  = 600
	minColumnHits    = 5
	minLinkHits      = 3
	minLinkShare     = 0.2
	minPortionShare  = 0.5
	minTitleDistinct = 0.7
)

var portionWords = []string{
	"full", "og file", "snippet", "partial", "n/a", "tagged", "beat only", "instrumental",
	"unconfirmed", "rumoured", "rumored", "unavailable", "stem", "acapella", "confirmed",
}

type columns struct {
	name, portion, quality int
	links                  []int
}

type columnStats struct {
	filled, links, videos, portions, numbers, yesNo int
	distinct                                        map[string]struct{}
}

func (c columns) usable() bool {
	return c.name >= 0 && c.portion >= 0 && len(c.links) > 0
}

func inferColumns(rows []gridRow) (columns, int, bool) {
	headerAt, hints := findHeaderHints(rows)
	stats := measureColumns(rows, headerAt+1, min(len(rows), headerAt+1+inferSampleRows))
	cols := columns{name: -1, portion: orHint(pickPortion(stats), hints.portion), quality: hints.quality}
	cols.links = pickLinks(stats)
	if len(cols.links) == 0 {
		cols.links = hints.links
	}
	cols.name = pickTitle(stats, hints.name, cols)
	return cols, headerAt, cols.usable()
}

func findHeaderHints(rows []gridRow) (int, columns) {
	for i := 0; i < min(len(rows), headerScanRows); i++ {
		if hints := headerHints(rows[i]); hints.name >= 0 || hints.portion >= 0 {
			return i, hints
		}
	}
	return -1, columns{name: -1, portion: -1, quality: -1}
}

func headerHints(header gridRow) columns {
	hints := columns{name: -1, portion: -1, quality: -1}
	for i, cell := range header.Values {
		label := strings.ToLower(firstLine(cell.FormattedValue))
		switch {
		case strings.HasPrefix(label, "name"), strings.HasPrefix(label, "title"):
			hints.name = i
		case strings.HasPrefix(label, "portion"), strings.Contains(label, "availab"):
			hints.portion = i
		case strings.HasPrefix(label, "quality"):
			hints.quality = i
		case strings.Contains(label, "link"):
			hints.links = append(hints.links, i)
		}
	}
	return hints
}

func measureColumns(rows []gridRow, from, to int) []columnStats {
	width := 0
	for i := from; i < to; i++ {
		width = max(width, len(rows[i].Values))
	}
	stats := make([]columnStats, width)
	for i := range stats {
		stats[i].distinct = map[string]struct{}{}
	}
	for i := from; i < to; i++ {
		for col, cell := range rows[i].Values {
			stats[col].add(cell)
		}
	}
	return stats
}

func (s *columnStats) add(cell gridCell) {
	text := firstLine(cell.FormattedValue)
	if text == "" && len(cell.links()) == 0 {
		return
	}
	s.filled++
	s.distinct[strings.ToLower(text)] = struct{}{}
	if hasLink(cell, audioLink) {
		s.links++
	} else if hasLink(cell, releasedLink) {
		s.videos++
	}
	if isPortionWord(text) {
		s.portions++
	}
	if streamingAnswer(text) != "" {
		s.yesNo++
	}
	if isNumeric(text) || isDate(text) {
		s.numbers++
	}
}

func hasLink(cell gridCell, read linkReader) bool {
	for _, raw := range cell.links() {
		if _, ok := read(raw); ok {
			return true
		}
	}
	return false
}

func isPortionWord(text string) bool {
	head, _ := splitPortion(text)
	head = strings.ToLower(strings.TrimLeft(head, "~ "))
	for _, word := range portionWords {
		if strings.HasPrefix(head, word) {
			return true
		}
	}
	return false
}

func isNumeric(text string) bool {
	return text != "" && strings.IndexFunc(text, notNumeric) < 0
}

var dateShape = regexp.MustCompile(`(?i)^(\d{1,4}[/.-]\d{1,2}[/.-]\d{1,4}|(jan|feb|mar|apr|may|jun|jul|aug|sep|oct|nov|dec)[a-z]*\.? (\d{1,2}|\?\?),? (\d{4}|\?\?\?\?))$`)

func isDate(text string) bool {
	return dateShape.MatchString(text)
}

func notNumeric(r rune) bool {
	return !unicode.IsDigit(r) && r != '.' && r != ':'
}

func orHint(inferred, hinted int) int {
	if inferred >= 0 {
		return inferred
	}
	return hinted
}

func pickPortion(stats []columnStats) int {
	best, bestHits := -1, 0
	for i, s := range stats {
		if s.portions >= minColumnHits && share(s.portions, s.filled) >= minPortionShare && s.portions > bestHits {
			best, bestHits = i, s.portions
		}
	}
	return best
}

func pickLinks(stats []columnStats) []int {
	return pickLinksBy(stats, func(s columnStats) int { return s.links })
}

func pickLinksBy(stats []columnStats, hits func(columnStats) int) []int {
	var links []int
	for i, s := range stats {
		if n := hits(s); n >= minLinkHits && share(n, s.filled) >= minLinkShare {
			links = append(links, i)
		}
	}
	return links
}

func pickTitle(stats []columnStats, hinted int, cols columns) int {
	if hinted >= 0 && cols.free(hinted) {
		return hinted
	}
	for i, s := range stats {
		if cols.free(i) && s.looksLikeTitles() {
			return i
		}
	}
	return -1
}

func (c columns) free(i int) bool {
	for _, link := range c.links {
		if link == i {
			return false
		}
	}
	return i != c.portion && i != c.quality
}

func (s columnStats) looksLikeTitles() bool {
	return s.filled >= minColumnHits && s.links+s.videos == 0 && s.numbers*2 < s.filled &&
		share(len(s.distinct), s.filled) >= minTitleDistinct
}

func share(part, whole int) float64 {
	if whole == 0 {
		return 0
	}
	return float64(part) / float64(whole)
}
