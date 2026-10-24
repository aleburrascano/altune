package trackers

import (
	"net/url"
	"regexp"
	"slices"
	"strings"
)

const (
	pixeldrainHost = "pixeldrain.com"
	headerScanRows = 5
)

var pixeldrainIDRe = regexp.MustCompile(`^[A-Za-z0-9]{6,16}$`)

type columns struct {
	name, portion, quality int
	links                  []int
}

type trackerRow struct {
	title       string
	quality     string
	pixeldrains []string
}

func mapColumns(header gridRow) columns {
	cols := columns{name: -1, portion: -1, quality: -1}
	for i, cell := range header.Values {
		label := strings.ToLower(firstLine(cell.FormattedValue))
		switch {
		case strings.HasPrefix(label, "name"):
			cols.name = i
		case strings.HasPrefix(label, "portion"), strings.HasPrefix(label, "available"):
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
	ids := c.pixeldrainIDs(row)
	if title == "" || len(ids) == 0 {
		return trackerRow{}, false
	}
	return trackerRow{title: title, quality: strings.TrimSpace(row.cell(c.quality).FormattedValue), pixeldrains: ids}, true
}

func isWholeSong(portion string) bool {
	p := strings.TrimSpace(portion)
	return strings.EqualFold(p, "full") || strings.EqualFold(p, "og file")
}

func (c columns) pixeldrainIDs(row gridRow) []string {
	var ids []string
	for _, col := range c.links {
		for _, link := range row.cell(col).links() {
			if id, ok := pixeldrainID(link); ok && !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

func pixeldrainID(link string) (string, bool) {
	u, err := url.Parse(link)
	if err != nil || !strings.EqualFold(strings.TrimPrefix(u.Hostname(), "www."), pixeldrainHost) {
		return "", false
	}
	id, found := strings.CutPrefix(u.Path, "/u/")
	if !found || !pixeldrainIDRe.MatchString(id) {
		return "", false
	}
	return id, true
}
