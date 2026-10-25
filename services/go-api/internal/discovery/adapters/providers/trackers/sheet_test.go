package trackers

import (
	"altune/go-api/internal/discovery/domain"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
)

func titlesOf(rows []trackerRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.title
	}
	return out
}

func TestParseSheet_KeepsOnlyWholeSongsWithAPixeldrainLink(t *testing.T) {
	rows, mapped, err := parseSheet(readFixture(t, "ken_carson_unreleased.json"))
	if err != nil || !mapped {
		t.Fatalf("parseSheet = (mapped %v, err %v)", mapped, err)
	}
	want := []string{"Dream [V2]", "Going Krazy", "Loot", "All Season [V1]"}
	if !slices.Equal(titlesOf(rows), want) {
		t.Errorf("titles = %v, want %v", titlesOf(rows), want)
	}
}

func TestParseSheet_ReadsQualityAndPixeldrainID(t *testing.T) {
	rows, _, err := parseSheet(readFixture(t, "ken_carson_unreleased.json"))
	if err != nil || len(rows) == 0 {
		t.Fatalf("rows = %v, err %v, want the sheet's songs", rows, err)
	}
	dream := rows[0]
	want := []domain.AudioLink{{Provider: "pixeldrain", ExternalID: "ozSpSjn7", URL: "https://pixeldrain.com/u/ozSpSjn7"}}
	if dream.quality != "High Quality" || !slices.Equal(dream.links, want) {
		t.Errorf("dream = %+v, want High Quality / ozSpSjn7", dream)
	}
}

func TestParseSheet_DropsSnippetsAndRowsWithoutALink(t *testing.T) {
	rows, _, _ := parseSheet(readFixture(t, "ken_carson_unreleased.json"))
	for _, dropped := range []string{"Maaly Raw*", "Dream [V1]", "32"} {
		if slices.Contains(titlesOf(rows), dropped) {
			t.Errorf("%q should be dropped", dropped)
		}
	}
}

func TestParseSheet_MapsASecondRealHeaderShapeByKeyword(t *testing.T) {
	rows, mapped, err := parseSheet(readFixture(t, "ariana_unreleased.json"))
	if err != nil || !mapped {
		t.Fatalf("parseSheet = (mapped %v, err %v)", mapped, err)
	}
	if len(rows) != 0 {
		t.Errorf("rows = %v, want none: that sheet's only link is not pixeldrain", rows)
	}
}

func TestParseSheet_UnknownLayoutIsUnmappedNotAnError(t *testing.T) {
	body := []byte(`{"sheets":[{"data":[{"rowData":[{"values":[{"formattedValue":"Foo"},{"formattedValue":"Bar"}]}]}]}]}`)
	rows, mapped, err := parseSheet(body)
	if err != nil || mapped || len(rows) != 0 {
		t.Errorf("parseSheet = (%v, %v, %v), want no rows, unmapped, no error", rows, mapped, err)
	}
}

func TestParseSheet_ReadsALinkHeldOnlyInATextRun(t *testing.T) {
	body := []byte(`{"sheets":[{"data":[{"rowData":[
{"values":[{"formattedValue":"Name"},{"formattedValue":"Portion"},{"formattedValue":"Alternate Links"}]},
{"values":[{"formattedValue":"Loot"},{"formattedValue":"OG File"},{"formattedValue":"Exo Pixel","textFormatRuns":[{"format":{"link":{"uri":"https://exoshare.org/share/x"}}},{"format":{"link":{"uri":"https://pixeldrain.com/u/abcd1234"}}}]}]}
]}]}]}`)
	rows, _, err := parseSheet(body)
	if err != nil || len(rows) != 1 || len(rows[0].links) != 1 || rows[0].links[0].ExternalID != "abcd1234" {
		t.Errorf("rows = %+v, err %v", rows, err)
	}
}

func TestAudioLink_ReadsEveryKnownFileHostAndSoundCloud(t *testing.T) {
	cases := map[string]domain.AudioLink{
		"https://pixeldrain.com/u/ozSpSjn7":                        {Provider: "pixeldrain", ExternalID: "ozSpSjn7", URL: "https://pixeldrain.com/u/ozSpSjn7"},
		"https://pillowcase.su/f/84310b5d2c21df85e4570aec299d1ead": {Provider: "pillows", ExternalID: "84310b5d2c21df85e4570aec299d1ead", URL: "https://pillows.su/f/84310b5d2c21df85e4570aec299d1ead"},
		"https://soundcloud.com/ken/dream?in=x":                    {Provider: "soundcloud", ExternalID: "https://soundcloud.com/ken/dream", URL: "https://soundcloud.com/ken/dream"},
	}
	for raw, want := range cases {
		if got, ok := audioLink(raw); !ok || got != want {
			t.Errorf("audioLink(%q) = %+v, %v, want %+v", raw, got, ok, want)
		}
	}
	for _, raw := range []string{"https://pixeldrain.com/u/../x", "https://pixeldrain.com.evil.io/u/abcdef", "https://youtube.com/watch?v=dQw4w9WgXcQ", "https://mega.nz/file/x"} {
		if got, ok := audioLink(raw); ok {
			t.Errorf("audioLink(%q) = %+v, want rejected", raw, got)
		}
	}
}

func gridOf(header []string, rows ...[]gridCell) []gridRow {
	head := gridRow{}
	for _, label := range header {
		head.Values = append(head.Values, gridCell{FormattedValue: label})
	}
	grid := []gridRow{head}
	for _, r := range rows {
		grid = append(grid, gridRow{Values: r})
	}
	return grid
}

func songRow(era, title, portion, link string) []gridCell {
	return []gridCell{{FormattedValue: era}, {FormattedValue: title}, {FormattedValue: portion}, {FormattedValue: "Pillowcase", Hyperlink: link}}
}

func pillowsLink(n int) string {
	return fmt.Sprintf("https://pillowcase.su/f/%032d", n)
}

func TestInferColumns_ReadsAnUnlabelledLayoutFromItsCells(t *testing.T) {
	var rows [][]gridCell
	for i := range 8 {
		rows = append(rows, songRow("Rodeo", fmt.Sprintf("Song %d\n(prod. X)", i), "Full - HQ", pillowsLink(i)))
	}
	cols, headerAt, ok := inferColumns(gridOf([]string{"Era", "Track", "What's There", "Sources"}, rows...))
	if !ok || headerAt != -1 || cols.name != 1 || cols.portion != 2 || len(cols.links) != 1 || cols.links[0] != 3 {
		t.Errorf("cols = %+v, header %d, ok %v; want title 1, portion 2, links [3]", cols, headerAt, ok)
	}
}

func TestInferColumns_PrefersAHintedTitleColumnAndSkipsNumbering(t *testing.T) {
	var rows [][]gridCell
	for i := range 8 {
		row := append([]gridCell{{FormattedValue: fmt.Sprint(i + 1)}}, songRow("Era", fmt.Sprintf("Song %d", i), "OG File", pillowsLink(i))...)
		rows = append(rows, row)
	}
	cols, _, ok := inferColumns(gridOf([]string{"#", "Era", "Title", "Portion", "Links"}, rows...))
	if !ok || cols.name != 2 || cols.portion != 3 {
		t.Errorf("cols = %+v, want the Title column (2) and Portion (3)", cols)
	}
}

func TestParseSheet_ReadsFullHQAsAWholeSongWithItsQuality(t *testing.T) {
	var rows [][]gridCell
	for i := range 6 {
		rows = append(rows, songRow("Era", fmt.Sprintf("Song %d", i), "Snippet - LQ", pillowsLink(i)))
	}
	rows = append(rows, songRow("Era", "Keeper", "Full - HQ", pillowsLink(99)), songRow("Era", "Fan cut", "Full - HQ (Unofficial)", pillowsLink(98)))
	body := gridBody(t, gridOf([]string{"Era", "Title", "What's Available?", "Links"}, rows...))
	got, mapped, err := parseSheet(body)
	if err != nil || !mapped || len(got) != 1 || got[0].title != "Keeper" || got[0].quality != "HQ" {
		t.Errorf("parseSheet = %+v, mapped %v, err %v; want only Keeper at HQ", got, mapped, err)
	}
}

func TestInferColumns_SkipsADateColumnLeftOfTheTitles(t *testing.T) {
	var rows [][]gridCell
	for i := range 8 {
		rows = append(rows, append([]gridCell{{FormattedValue: fmt.Sprintf("Feb %d, 2017", i+1)}}, songRow("Era", fmt.Sprintf("Song %d", i), "Full", pillowsLink(i))...))
	}
	cols, _, ok := inferColumns(gridOf([]string{"Date", "Era", "Track", "Portion", "Links"}, rows...))
	if !ok || cols.name != 2 {
		t.Errorf("cols = %+v, want the Track column (2), not the dates", cols)
	}
}

func TestInferColumns_UsesHeaderWordsWhenASheetIsTooSmallToJudge(t *testing.T) {
	grid := gridOf([]string{"Name", "Portion", "Links"}, []gridCell{{FormattedValue: "Loot"}, {FormattedValue: "OG File"}, {FormattedValue: "x", Hyperlink: "https://pixeldrain.com/u/abcd1234"}})
	cols, headerAt, ok := inferColumns(grid)
	if !ok || headerAt != 0 || cols.name != 0 || cols.portion != 1 || len(cols.links) != 1 || cols.links[0] != 2 {
		t.Errorf("cols = %+v, header %d, ok %v; want the header's Name, Portion and Links", cols, headerAt, ok)
	}
}

func gridBody(t *testing.T, grid []gridRow) []byte {
	t.Helper()
	var resp gridResponse
	resp.Sheets = make([]struct {
		Data []struct {
			RowData []gridRow `json:"rowData"`
		} `json:"data"`
	}, 1)
	resp.Sheets[0].Data = []struct {
		RowData []gridRow `json:"rowData"`
	}{{RowData: grid}}
	body, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
