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

func openURLSheet(t *testing.T, extra ...[]gridCell) []byte {
	t.Helper()
	var rows [][]gridCell
	for i := range 6 {
		rows = append(rows, songRow("Era", fmt.Sprintf("Filler %d", i), "Full - HQ", pillowsLink(i)))
	}
	rows = append(rows, extra...)
	return gridBody(t, gridOf([]string{"Era", "Title", "What's Available?", "Links"}, rows...))
}

func TestParseSheet_OpenURLKeepsAWholeSongWithOnlyAnUnsupportedHttpsLink(t *testing.T) {
	body := openURLSheet(t,
		songRow("Era", "Song X", "Full", "https://mega.nz/file/abc"),
		songRow("Era", "Bad Scheme", "Full", "intent://x"),
		songRow("Era", "Snip", "Snippet", "https://mega.nz/file/def"),
	)
	rows, mapped, err := parseSheet(body)
	if err != nil || !mapped {
		t.Fatalf("parseSheet = mapped %v, err %v", mapped, err)
	}
	var got []domain.UnreleasedSong
	for _, s := range songsFromRows("Artist", rows) {
		if s.Title == "Song X" || s.Title == "Bad Scheme" || s.Title == "Snip" {
			got = append(got, s)
		}
	}
	if len(got) != 1 || got[0].Title != "Song X" {
		t.Fatalf("songs = %+v, want only Song X", got)
	}
	s := got[0]
	if len(s.AudioLinks) != 0 || s.HasMedia == nil || *s.HasMedia || s.OpenURL != "https://mega.nz/file/abc" || s.OpenHost != "mega.nz" {
		t.Errorf("song = %+v, want no audio, has_media false, open mega.nz", s)
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

func TestSongsFromRows_ALinkOutRowFollowedByARowWithLinksGainsMediaAndKeepsItsOpenURL(t *testing.T) {
	link := domain.AudioLink{Provider: "pixeldrain", ExternalID: "abc", URL: "https://pixeldrain.com/u/abc"}
	songs := songsFromRows("Artist", []trackerRow{
		{title: "Song X", openURL: "https://mega.nz/file/abc", openHost: "mega.nz"},
		{title: "Song X", links: []domain.AudioLink{link}, offStreaming: true},
	})
	if len(songs) != 1 {
		t.Fatalf("songs = %+v, want one deduped song", songs)
	}
	s := songs[0]
	if s.HasMedia == nil || !*s.HasMedia || len(s.AudioLinks) != 1 || s.OpenURL != "https://mega.nz/file/abc" || !s.OffStreaming {
		t.Errorf("song = %+v, want has_media true, the pixeldrain link, the mega open link and off_streaming", s)
	}
}

func TestFirstHTTPSLink_OpenHostDropsThePort(t *testing.T) {
	body := openURLSheet(t, songRow("Era", "Ported", "Full", "https://files.example.com:8443/a"))
	rows, _, err := parseSheet(body)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.title == "Ported" {
			if r.openHost != "files.example.com" {
				t.Errorf("openHost = %q, want files.example.com", r.openHost)
			}
			return
		}
	}
	t.Fatal("row Ported was not parsed")
}

func versionLink(id string) []domain.AudioLink {
	return []domain.AudioLink{{Provider: "pixeldrain", ExternalID: id, URL: "https://pixeldrain.com/u/" + id}}
}

func TestSongsFromRows_VersionTaggedRowsBecomeOneSongWithTheBestVersionsLinks(t *testing.T) {
	songs := songsFromRows("Tate McRae", []trackerRow{
		{title: "Sports Car [V3]", quality: "CD Quality", links: versionLink("v3")},
		{title: "Sports Car [V10]", quality: "Lossless", links: versionLink("v10")},
		{title: "Sports Car [V14]", quality: "CD Quality", links: versionLink("v14")},
	})
	if len(songs) != 1 {
		t.Fatalf("songs = %+v, want one", songs)
	}
	s := songs[0]
	if s.Title != "Sports Car" || s.TrackerKey != "tate-mcrae:sports-car" || len(s.Versions) != 3 {
		t.Fatalf("song = %+v, want base title, base key and 3 versions", s)
	}
	if s.Quality != "Lossless" || !slices.Equal(s.AudioLinks, versionLink("v10")) || s.HasMedia == nil || !*s.HasMedia {
		t.Errorf("song = %+v, want V10's quality, links and media", s)
	}
	if s.Versions[0].Title != "Sports Car [V3]" || s.Versions[0].Label != "V3" {
		t.Errorf("first version = %+v, want the full sheet title and V3", s.Versions[0])
	}
}

func TestSongsFromRows_EqualQualityPrefersTheHighestVersionNumber(t *testing.T) {
	songs := songsFromRows("A", []trackerRow{
		{title: "Song [V9]", quality: "High Quality", links: versionLink("v9")},
		{title: "Song [V12]", quality: "high quality", links: versionLink("v12")},
		{title: "Song [V2]", quality: "High Quality", links: versionLink("v2")},
	})
	if len(songs) != 1 || !slices.Equal(songs[0].AudioLinks, versionLink("v12")) {
		t.Errorf("songs = %+v, want one song with V12's links", songs)
	}
}

func TestSongsFromRows_DifferentBaseTitlesStayApartAndReleasedRowsAreNotVersions(t *testing.T) {
	songs := songsFromRows("A", []trackerRow{
		{title: "Signs [V1]", links: versionLink("a")},
		{title: "Siren Sounds [V1]", links: versionLink("b")},
		{title: "Signs", links: versionLink("c"), offStreaming: true},
	})
	if len(songs) != 2 {
		t.Fatalf("songs = %+v, want two", songs)
	}
	signs := songs[0]
	if len(signs.Versions) != 1 || !signs.OffStreaming || len(signs.AudioLinks) != 2 {
		t.Errorf("signs = %+v, want one version, off_streaming and both links", signs)
	}
}

func TestSongsFromRows_AVariantTitleIsNotFoldedIntoTheTaggedSong(t *testing.T) {
	songs := songsFromRows("A", []trackerRow{
		{title: "Song [V1]", quality: "CD Quality", links: versionLink("v1")},
		{title: "Song (Instrumental)", quality: "Lossless", links: versionLink("inst")},
	})
	if len(songs) != 2 {
		t.Fatalf("songs = %+v, want two", songs)
	}
	if songs[0].Title != "Song" || !slices.Equal(songs[0].AudioLinks, versionLink("v1")) || len(songs[0].Versions) != 1 {
		t.Errorf("song = %+v, want Song with only V1's links", songs[0])
	}
}
