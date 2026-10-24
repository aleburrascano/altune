package trackers

import (
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
	if dream.quality != "High Quality" || !slices.Equal(dream.pixeldrains, []string{"ozSpSjn7"}) {
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

func TestMapColumns_FindsColumnsWhateverTheirPosition(t *testing.T) {
	header := gridRow{Values: []gridCell{
		{FormattedValue: "Link(s)"}, {FormattedValue: "Portion"}, {FormattedValue: "Quality"}, {FormattedValue: "Name"},
	}}
	cols := mapColumns(header)
	if cols.name != 3 || cols.portion != 1 || cols.quality != 2 || !slices.Equal(cols.links, []int{0}) {
		t.Errorf("cols = %+v", cols)
	}
}

func TestMapColumns_ReadsAnAvailabilityColumnAsThePortion(t *testing.T) {
	header := gridRow{Values: []gridCell{
		{FormattedValue: "Name "}, {FormattedValue: "Availability"}, {FormattedValue: "Links\n(pillows.su)"},
	}}
	if cols := mapColumns(header); cols.portion != 1 || !cols.usable() {
		t.Errorf("cols = %+v, want Availability as the portion column", cols)
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
	if err != nil || len(rows) != 1 || !slices.Equal(rows[0].pixeldrains, []string{"abcd1234"}) {
		t.Errorf("rows = %+v, err %v", rows, err)
	}
}

func TestPixeldrainID_RejectsOtherHostsAndOddIDs(t *testing.T) {
	cases := map[string]bool{
		"https://pixeldrain.com/u/ozSpSjn7":       true,
		"https://pixeldrain.com/l/ozSpSjn7":       false,
		"https://evil.example/u/ozSpSjn7":         false,
		"https://pixeldrain.com/u/../x":           false,
		"https://pixeldrain.com.evil.io/u/abcdef": false,
	}
	for link, want := range cases {
		if _, ok := pixeldrainID(link); ok != want {
			t.Errorf("pixeldrainID(%q) ok = %v, want %v", link, ok, want)
		}
	}
}
