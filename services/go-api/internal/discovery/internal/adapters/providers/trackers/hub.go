package trackers

import (
	"altune/go-api/internal/shared/textnorm"
	"net/url"
	"regexp"
	"strings"
)

const sheetsDocHost = "docs.google.com"

var sheetIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{20,}$`)

func parseHub(body []byte) (map[string]string, error) {
	rows, err := decodeRows(body)
	if err != nil {
		return nil, err
	}
	index := make(map[string]string, len(rows))
	for _, row := range rows {
		name, sheetID, ok := hubEntry(row.cell(0))
		if _, taken := index[name]; !ok || taken {
			continue
		}
		index[name] = sheetID
	}
	return index, nil
}

func hubEntry(c gridCell) (name, sheetID string, ok bool) {
	name = textnorm.NormalizeForIdentity(trimLeadingDecoration(c.FormattedValue))
	if name == "" {
		return "", "", false
	}
	for _, link := range c.links() {
		if id, found := sheetIDFromLink(link); found {
			return name, id, true
		}
	}
	return "", "", false
}

func sheetIDFromLink(link string) (string, bool) {
	u, err := url.Parse(link)
	if err != nil || !strings.EqualFold(u.Hostname(), sheetsDocHost) {
		return "", false
	}
	segments := strings.Split(u.Path, "/")
	for i := 0; i+1 < len(segments); i++ {
		if segments[i] == "d" && sheetIDRe.MatchString(segments[i+1]) {
			return segments[i+1], true
		}
	}
	return "", false
}
