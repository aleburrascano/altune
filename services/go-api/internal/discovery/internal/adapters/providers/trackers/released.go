package trackers

import (
	"altune/go-api/internal/discovery/domain"
	"strings"
)

func parseReleased(body []byte) (rows []trackerRow, mapped bool, err error) {
	grid, err := decodeRows(body)
	if err != nil {
		return nil, false, err
	}
	cols, headerAt, mapped := inferReleasedColumns(grid)
	if !mapped {
		return nil, false, nil
	}
	for i := headerAt + 1; i < len(grid); i++ {
		if parsed, ok := cols.parseReleasedRow(grid[i]); ok {
			rows = append(rows, parsed)
		}
	}
	return rows, true, nil
}

func inferReleasedColumns(rows []gridRow) (columns, int, bool) {
	headerAt, hints := findHeaderHints(rows)
	stats := measureColumns(rows, headerAt+1, min(len(rows), headerAt+1+inferSampleRows))
	cols := columns{name: -1, portion: pickStreaming(stats), quality: -1, links: pickLinksBy(stats, playableLinks)}
	if len(cols.links) == 0 {
		cols.links = hints.links
	}
	cols.name = pickTitle(stats, hints.name, cols)
	return cols, headerAt, cols.usable()
}

func playableLinks(s columnStats) int { return s.links + s.videos }

func (c columns) parseReleasedRow(row gridRow) (trackerRow, bool) {
	if streamingAnswer(row.cell(c.portion).FormattedValue) != "no" {
		return trackerRow{}, false
	}
	title := trimLeadingDecoration(firstLine(row.cell(c.name).FormattedValue))
	links := c.audioLinks(row, releasedLink)
	if title == "" || len(links) == 0 {
		return trackerRow{}, false
	}
	return trackerRow{title: title, links: links, offStreaming: true}, true
}

func pickStreaming(stats []columnStats) int {
	best, bestHits := -1, 0
	for i, s := range stats {
		if s.yesNo >= minColumnHits && share(s.yesNo, s.filled) >= minPortionShare && s.yesNo > bestHits {
			best, bestHits = i, s.yesNo
		}
	}
	return best
}

func streamingAnswer(text string) string {
	word, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(firstLine(text))), " ")
	if word == "yes" || word == "no" {
		return word
	}
	return ""
}

func releasedLink(raw string) (domain.AudioLink, bool) {
	if link, ok := domain.YouTubeLink(raw); ok {
		return link, true
	}
	return audioLink(raw)
}
