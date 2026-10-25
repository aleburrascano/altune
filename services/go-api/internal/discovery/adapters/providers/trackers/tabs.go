package trackers

import (
	"encoding/json"
	"strings"
	"unicode"
)

const (
	tabsMask       = "sheets.properties.title"
	unreleasedWord = "unreleased"
	releasedWord   = "released"
)

type sheetTabs struct {
	unreleased, released string
}

type tabsResponse struct {
	Sheets []struct {
		Properties struct {
			Title string `json:"title"`
		} `json:"properties"`
	} `json:"sheets"`
}

func pickTabs(body []byte) (sheetTabs, error) {
	var resp tabsResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return sheetTabs{}, err
	}
	titles := make([]string, 0, len(resp.Sheets))
	for _, sheet := range resp.Sheets {
		titles = append(titles, sheet.Properties.Title)
	}
	unreleased, _ := pickUnreleased(titles)
	return sheetTabs{unreleased: unreleased, released: pickReleased(titles)}, nil
}

func pickUnreleased(titles []string) (string, bool) {
	for _, title := range titles {
		if lettersOnly(title) == unreleasedWord {
			return title, true
		}
	}
	for _, title := range titles {
		if strings.Contains(lettersOnly(title), unreleasedWord) {
			return title, true
		}
	}
	return "", false
}

func pickReleased(titles []string) string {
	for _, title := range titles {
		if lettersOnly(title) == releasedWord {
			return title
		}
	}
	return ""
}

func lettersOnly(title string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, title)
}

func tabRange(title string) string {
	return "'" + strings.ReplaceAll(title, "'", "''") + "'"
}
