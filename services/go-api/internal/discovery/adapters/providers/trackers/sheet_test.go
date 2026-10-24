package trackers

import (
	"altune/go-api/internal/discovery/domain"
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
