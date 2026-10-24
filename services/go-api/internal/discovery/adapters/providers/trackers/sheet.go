package trackers

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/filehost"
	"slices"
	"strings"
)

const headerScanRows = 5

type columns struct {
	name, portion, quality int
	links                  []int
}

type trackerRow struct {
	title   string
	quality string
	links   []domain.AudioLink
}

func mapColumns(header gridRow) columns {
	cols := columns{name: -1, portion: -1, quality: -1}
	for i, cell := range header.Values {
		label := strings.ToLower(firstLine(cell.FormattedValue))
		switch {
		case strings.HasPrefix(label, "name"):
			cols.name = i
		case strings.HasPrefix(label, "portion"), strings.HasPrefix(label, "availab"):
			cols.portion = i
		case strings.HasPrefix(label, "quality"):
			cols.quality = i
		case strings.Contains(label, "link"):
			cols.links = append(cols.links, i)
		}
	}
	return cols
}

func (c columns) usable() bool {
	return c.name >= 0 && c.portion >= 0 && len(c.links) > 0
}

func findHeader(rows []gridRow) (columns, int, bool) {
	for i := 0; i < min(len(rows), headerScanRows); i++ {
		if cols := mapColumns(rows[i]); cols.usable() {
			return cols, i, true
		}
	}
	return columns{}, 0, false
}

func parseSheet(body []byte) (rows []trackerRow, mapped bool, err error) {
	grid, err := decodeRows(body)
	if err != nil {
		return nil, false, err
	}
	cols, headerAt, mapped := findHeader(grid)
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
	if !isWholeSong(row.cell(c.portion).FormattedValue) {
		return trackerRow{}, false
	}
	title := trimLeadingDecoration(firstLine(row.cell(c.name).FormattedValue))
	links := c.audioLinks(row)
	if title == "" || len(links) == 0 {
		return trackerRow{}, false
	}
	return trackerRow{title: title, quality: strings.TrimSpace(row.cell(c.quality).FormattedValue), links: links}, true
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
