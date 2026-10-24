package trackers

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/filehost"
	"slices"
	"strings"
)

type trackerRow struct {
	title   string
	quality string
	links   []domain.AudioLink
}

func parseSheet(body []byte) (rows []trackerRow, mapped bool, err error) {
	grid, err := decodeRows(body)
	if err != nil {
		return nil, false, err
	}
	cols, headerAt, mapped := inferColumns(grid)
	if !mapped {
		return nil, false, nil
	}
	for i := headerAt + 1; i < len(grid); i++ {
		if parsed, ok := cols.parseRow(grid[i]); ok {
			rows = append(rows, parsed)
		}
	}
	return rows, true, nil
}

func (c columns) parseRow(row gridRow) (trackerRow, bool) {
	portion, portionQuality := splitPortion(row.cell(c.portion).FormattedValue)
	if !isWholeSong(portion) {
		return trackerRow{}, false
	}
	title := trimLeadingDecoration(firstLine(row.cell(c.name).FormattedValue))
	links := c.audioLinks(row)
	if title == "" || len(links) == 0 {
		return trackerRow{}, false
	}
	quality := strings.TrimSpace(row.cell(c.quality).FormattedValue)
	if c.quality < 0 {
		quality = portionQuality
	}
	return trackerRow{title: title, quality: quality, links: links}, true
}

func splitPortion(raw string) (portion, quality string) {
	line := strings.TrimSpace(firstLine(raw))
	if strings.Contains(strings.ToLower(line), "unofficial") {
		return "", ""
	}
	portion, quality, _ = strings.Cut(line, " - ")
	return portion, strings.TrimSpace(quality)
}

func isWholeSong(portion string) bool {
	p := strings.TrimSpace(portion)
	return strings.EqualFold(p, "full") || strings.EqualFold(p, "og file")
}

func (c columns) audioLinks(row gridRow) []domain.AudioLink {
	var links []domain.AudioLink
	for _, col := range c.links {
		for _, raw := range row.cell(col).links() {
			if link, ok := audioLink(raw); ok && !slices.Contains(links, link) {
				links = append(links, link)
			}
		}
	}
	return links
}

func audioLink(raw string) (domain.AudioLink, bool) {
	if file, ok := filehost.Parse(raw); ok {
		return domain.AudioLink{Provider: domain.AudioProvider(file.Provider), ExternalID: file.ID, URL: file.PageURL}, true
	}
	return domain.SoundCloudLink(raw)
}
