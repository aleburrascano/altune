package trackers

import (
	"encoding/json"
	"slices"
	"strings"
	"unicode"
)

type gridCell struct {
	FormattedValue string `json:"formattedValue"`
	Hyperlink      string `json:"hyperlink"`
	TextFormatRuns []struct {
		Format struct {
			Link struct {
				URI string `json:"uri"`
			} `json:"link"`
		} `json:"format"`
	} `json:"textFormatRuns"`
}

type gridRow struct {
	Values []gridCell `json:"values"`
}

type gridResponse struct {
	Sheets []struct {
		Data []struct {
			RowData []gridRow `json:"rowData"`
		} `json:"data"`
	} `json:"sheets"`
}

func decodeRows(body []byte) ([]gridRow, error) {
	var resp gridResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	if len(resp.Sheets) == 0 || len(resp.Sheets[0].Data) == 0 {
		return []gridRow{}, nil
	}
	return resp.Sheets[0].Data[0].RowData, nil
}

func (c gridCell) links() []string {
	var out []string
	if c.Hyperlink != "" {
		out = append(out, c.Hyperlink)
	}
	for _, run := range c.TextFormatRuns {
		if uri := run.Format.Link.URI; uri != "" && !slices.Contains(out, uri) {
			out = append(out, uri)
		}
	}
	return out
}

func (r gridRow) cell(i int) gridCell {
	if i < 0 || i >= len(r.Values) {
		return gridCell{}
	}
	return r.Values[i]
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}

func trimLeadingDecoration(s string) string {
	return strings.TrimLeftFunc(s, isDecoration)
}

func isDecoration(r rune) bool {
	return unicode.In(r, unicode.So, unicode.Sk, unicode.Mn, unicode.Cf, unicode.Z, unicode.Cc)
}
