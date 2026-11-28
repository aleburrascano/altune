package fansheets_test

import (
	"altune/go-api/internal/search/internal/domain"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
)

type cell struct {
	value, link string
	runLinks    []string
}

func (c cell) MarshalJSON() ([]byte, error) {
	out := map[string]any{"formattedValue": c.value}
	if c.link != "" {
		out["hyperlink"] = c.link
	}
	if len(c.runLinks) > 0 {
		runs := make([]any, 0, len(c.runLinks))
		for _, uri := range c.runLinks {
			runs = append(runs, map[string]any{"format": map[string]any{"link": map[string]any{"uri": uri}}})
		}
		out["textFormatRuns"] = runs
	}
	return json.Marshal(out)
}

func gridBody(t *testing.T, header []string, rows ...[]cell) []byte {
	t.Helper()
	head := make([]cell, 0, len(header))
	for _, label := range header {
		head = append(head, cell{value: label})
	}
	rowData := []any{map[string]any{"values": head}}
	for _, r := range rows {
		rowData = append(rowData, map[string]any{"values": r})
	}
	body, err := json.Marshal(map[string]any{"sheets": []any{map[string]any{"data": []any{map[string]any{"rowData": rowData}}}}})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func songRow(era, title, portion, link string) []cell {
	return []cell{{value: era}, {value: title}, {value: portion}, {value: "Pillowcase", link: link}}
}

func pillowsLink(n int) string {
	return fmt.Sprintf("https://pillowcase.su/f/%032d", n)
}

func pixeldrain(id string) []domain.AudioLink {
	return []domain.AudioLink{{Provider: "pixeldrain", ExternalID: id, URL: "https://pixeldrain.com/u/" + id}}
}

func trackerSongs(t *testing.T, tabs string, unreleased, released []byte) []domain.UnreleasedSong {
	t.Helper()
	srv := newSheetsServer(t, readFixture(t, "hub_artists.json"), unreleased)
	srv.tabs = tabs
	if released != nil {
		srv.released, srv.releasedCode = released, http.StatusOK
	}
	songs, err := srv.client(t, nil).ArtistSongs(context.Background(), "Ken Carson")
	if err != nil {
		t.Fatalf("ArtistSongs: %v", err)
	}
	return songs
}

func unreleasedSongs(t *testing.T, body []byte) []domain.UnreleasedSong {
	t.Helper()
	return trackerSongs(t, tabsBody("Unreleased"), body, nil)
}

func releasedSongs(t *testing.T, body []byte) []domain.UnreleasedSong {
	t.Helper()
	return trackerSongs(t, tabsBody("Released"), nil, body)
}

func versionTitles(songs []domain.UnreleasedSong) []string {
	var out []string
	for _, s := range songs {
		for _, v := range s.Versions {
			out = append(out, v.Title)
		}
	}
	return out
}

func songTitled(t *testing.T, songs []domain.UnreleasedSong, title string) domain.UnreleasedSong {
	t.Helper()
	for _, s := range songs {
		if s.Title == title {
			return s
		}
	}
	t.Fatalf("no song %q in %+v", title, songs)
	return domain.UnreleasedSong{}
}

func TestSheet_KeepsOnlyWholeSongsWithAPixeldrainLink(t *testing.T) {
	songs := unreleasedSongs(t, readFixture(t, "ken_carson_unreleased.json"))
	want := []string{"Dream [V2]", "Going Krazy", "Loot", "All Season [V1]"}
	if got := versionTitles(songs); !slices.Equal(got, want) {
		t.Errorf("rows = %v, want %v", got, want)
	}
}

func TestSheet_ReadsQualityAndPixeldrainID(t *testing.T) {
	songs := unreleasedSongs(t, readFixture(t, "ken_carson_unreleased.json"))
	if len(songs) == 0 || len(songs[0].Versions) == 0 {
		t.Fatalf("songs = %+v, want the sheet's songs", songs)
	}
	dream := songs[0].Versions[0]
	if dream.Quality != "High Quality" || !slices.Equal(dream.AudioLinks, pixeldrain("ozSpSjn7")) {
		t.Errorf("dream = %+v, want High Quality / ozSpSjn7", dream)
	}
}

func TestSheet_DropsSnippetsAndRowsWithoutALink(t *testing.T) {
	rows := versionTitles(unreleasedSongs(t, readFixture(t, "ken_carson_unreleased.json")))
	for _, dropped := range []string{"Maaly Raw*", "Dream [V1]", "32"} {
		if slices.Contains(rows, dropped) {
			t.Errorf("%q should be dropped", dropped)
		}
	}
}

func TestSheet_MapsASecondRealHeaderShapeByKeyword(t *testing.T) {
	logged := captureLog(t)
	if songs := unreleasedSongs(t, readFixture(t, "ariana_unreleased.json")); len(songs) != 0 {
		t.Errorf("songs = %+v, want none: that sheet's only link is not pixeldrain", songs)
	}
	if strings.Contains(logged.String(), "trackers.unmapped_sheet") {
		t.Errorf("log = %q, want the sheet mapped by its header keywords", logged.String())
	}
}

func TestSheet_UnknownLayoutIsUnmappedNotAnError(t *testing.T) {
	logged := captureLog(t)
	body := []byte(`{"sheets":[{"data":[{"rowData":[{"values":[{"formattedValue":"Foo"},{"formattedValue":"Bar"}]}]}]}]}`)
	if songs := unreleasedSongs(t, body); len(songs) != 0 {
		t.Errorf("songs = %+v, want none", songs)
	}
	if !strings.Contains(logged.String(), "trackers.unmapped_sheet") {
		t.Errorf("log = %q, want the sheet reported as unmapped", logged.String())
	}
}

func TestSheet_ReadsALinkHeldOnlyInATextRun(t *testing.T) {
	body := gridBody(t, []string{"Name", "Portion", "Alternate Links"},
		[]cell{{value: "Loot"}, {value: "OG File"}, {value: "Exo Pixel", runLinks: []string{"https://exoshare.org/share/x", "https://pixeldrain.com/u/abcd1234"}}})
	songs := unreleasedSongs(t, body)
	if len(songs) != 1 || len(songs[0].AudioLinks) != 1 || songs[0].AudioLinks[0].ExternalID != "abcd1234" {
		t.Errorf("songs = %+v", songs)
	}
}

func oneLinkSheet(t *testing.T, raw string) []byte {
	t.Helper()
	return gridBody(t, []string{"Name", "Portion", "Links"}, []cell{{value: "Loot"}, {value: "OG File"}, {value: "x", link: raw}})
}

func TestSheet_ReadsEveryKnownFileHostAndSoundCloud(t *testing.T) {
	cases := map[string]domain.AudioLink{
		"https://pixeldrain.com/u/ozSpSjn7":                        {Provider: "pixeldrain", ExternalID: "ozSpSjn7", URL: "https://pixeldrain.com/u/ozSpSjn7"},
		"https://pillowcase.su/f/84310b5d2c21df85e4570aec299d1ead": {Provider: "pillows", ExternalID: "84310b5d2c21df85e4570aec299d1ead", URL: "https://pillows.su/f/84310b5d2c21df85e4570aec299d1ead"},
		"https://soundcloud.com/ken/dream?in=x":                    {Provider: "soundcloud", ExternalID: "https://soundcloud.com/ken/dream", URL: "https://soundcloud.com/ken/dream"},
	}
	for raw, want := range cases {
		songs := unreleasedSongs(t, oneLinkSheet(t, raw))
		if len(songs) != 1 || !slices.Equal(songs[0].AudioLinks, []domain.AudioLink{want}) {
			t.Errorf("link %q gave songs %+v, want the one link %+v", raw, songs, want)
		}
	}
	for _, raw := range []string{"https://pixeldrain.com/u/../x", "https://pixeldrain.com.evil.io/u/abcdef", "https://youtube.com/watch?v=dQw4w9WgXcQ", "https://mega.nz/file/x"} {
		for _, s := range unreleasedSongs(t, oneLinkSheet(t, raw)) {
			if len(s.AudioLinks) != 0 {
				t.Errorf("link %q gave audio %+v, want it rejected", raw, s.AudioLinks)
			}
		}
	}
}

func TestSheet_ReadsAnUnlabelledLayoutFromItsCells(t *testing.T) {
	var rows [][]cell
	var want []string
	for i := range 8 {
		rows = append(rows, songRow("Rodeo", fmt.Sprintf("Song %d\n(prod. X)", i), "Full - HQ", pillowsLink(i)))
		want = append(want, fmt.Sprintf("Song %d", i))
	}
	songs := unreleasedSongs(t, gridBody(t, []string{"Era", "Track", "What's There", "Sources"}, rows...))
	var got []string
	for _, s := range songs {
		got = append(got, s.Title)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("titles = %q, want the Track column read as titles: %q", got, want)
	}
	if songs[3].Quality != "HQ" || songs[3].AudioLinks[0].Provider != "pillows" {
		t.Errorf("song = %+v, want the portion's HQ quality and the Sources link", songs[3])
	}
}

func TestSheet_PrefersAHintedTitleColumnAndSkipsNumbering(t *testing.T) {
	var rows [][]cell
	for i := range 8 {
		rows = append(rows, append([]cell{{value: fmt.Sprint(i + 1)}}, songRow("Era", fmt.Sprintf("Song %d", i), "OG File", pillowsLink(i))...))
	}
	songs := unreleasedSongs(t, gridBody(t, []string{"#", "Era", "Title", "Portion", "Links"}, rows...))
	if len(songs) != 8 || songs[0].Title != "Song 0" || songs[7].Title != "Song 7" {
		t.Errorf("songs = %+v, want the Title column read as titles", songs)
	}
}

func TestSheet_ReadsFullHQAsAWholeSongWithItsQuality(t *testing.T) {
	var rows [][]cell
	for i := range 6 {
		rows = append(rows, songRow("Era", fmt.Sprintf("Song %d", i), "Snippet - LQ", pillowsLink(i)))
	}
	rows = append(rows, songRow("Era", "Keeper", "Full - HQ", pillowsLink(99)), songRow("Era", "Fan cut", "Full - HQ (Unofficial)", pillowsLink(98)))
	songs := unreleasedSongs(t, gridBody(t, []string{"Era", "Title", "What's Available?", "Links"}, rows...))
	if len(songs) != 1 || songs[0].Title != "Keeper" || songs[0].Quality != "HQ" {
		t.Errorf("songs = %+v; want only Keeper at HQ", songs)
	}
}

func TestSheet_SkipsADateColumnLeftOfTheTitles(t *testing.T) {
	var rows [][]cell
	for i := range 8 {
		rows = append(rows, append([]cell{{value: fmt.Sprintf("Feb %d, 2017", i+1)}}, songRow("Era", fmt.Sprintf("Song %d", i), "Full", pillowsLink(i))...))
	}
	songs := unreleasedSongs(t, gridBody(t, []string{"Date", "Era", "Track", "Portion", "Links"}, rows...))
	if len(songs) != 8 || songs[0].Title != "Song 0" {
		t.Errorf("songs = %+v, want the Track column, not the dates", songs)
	}
}

func TestSheet_UsesHeaderWordsWhenASheetIsTooSmallToJudge(t *testing.T) {
	songs := unreleasedSongs(t, oneLinkSheet(t, "https://pixeldrain.com/u/abcd1234"))
	if len(songs) != 1 || songs[0].Title != "Loot" || !slices.Equal(songs[0].AudioLinks, pixeldrain("abcd1234")) {
		t.Errorf("songs = %+v, want Loot read through the header's Name, Portion and Links", songs)
	}
}

func openURLSheet(t *testing.T, extra ...[]cell) []byte {
	t.Helper()
	var rows [][]cell
	for i := range 6 {
		rows = append(rows, songRow("Era", fmt.Sprintf("Filler %d", i), "Full - HQ", pillowsLink(i)))
	}
	rows = append(rows, extra...)
	return gridBody(t, []string{"Era", "Title", "What's Available?", "Links"}, rows...)
}

func TestSheet_OpenURLKeepsAWholeSongWithOnlyAnUnsupportedHttpsLink(t *testing.T) {
	songs := unreleasedSongs(t, openURLSheet(t,
		songRow("Era", "Song X", "Full", "https://mega.nz/file/abc"),
		songRow("Era", "Bad Scheme", "Full", "intent://x"),
		songRow("Era", "Snip", "Snippet", "https://mega.nz/file/def"),
	))
	var got []domain.UnreleasedSong
	for _, s := range songs {
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

func offStreamingRows(extra ...[]cell) [][]cell {
	var rows [][]cell
	for i := range 6 {
		rows = append(rows, releasedRow(fmt.Sprintf("Hit %d", i), "Yes", fmt.Sprintf("https://youtu.be/hit%08d", i)))
	}
	return append(rows, extra...)
}

func releasedHeader() []string {
	return []string{"Era", "Name", "Type", "Streaming", "Link(s)"}
}

func TestSongs_ALinkOutRowFollowedByAReleasedRowWithLinksGainsMediaAndKeepsItsOpenURL(t *testing.T) {
	unreleased := openURLSheet(t, songRow("Era", "Song X", "Full", "https://mega.nz/file/abc"))
	released := gridBody(t, releasedHeader(), offStreamingRows(releasedRow("Song X", "No", "https://pixeldrain.com/u/abcd0001"))...)
	songs := trackerSongs(t, tabsBody("Unreleased", "Released"), unreleased, released)

	s := songTitled(t, songs, "Song X")
	if s.HasMedia == nil || !*s.HasMedia || !slices.Equal(s.AudioLinks, pixeldrain("abcd0001")) || s.OpenURL != "https://mega.nz/file/abc" || !s.OffStreaming {
		t.Errorf("song = %+v, want has_media true, the pixeldrain link, the mega open link and off_streaming", s)
	}
	if n := strings.Count(strings.Join(versionTitles(songs), "|"), "Song X"); n != 1 {
		t.Errorf("Song X appears in %d versions, want the released row folded into the one song", n)
	}
}

func TestSheet_OpenHostDropsThePort(t *testing.T) {
	songs := unreleasedSongs(t, openURLSheet(t, songRow("Era", "Ported", "Full", "https://files.example.com:8443/a")))
	if s := songTitled(t, songs, "Ported"); s.OpenHost != "files.example.com" {
		t.Errorf("openHost = %q, want files.example.com", s.OpenHost)
	}
}

func versionSheet(t *testing.T, rows ...[]cell) []byte {
	t.Helper()
	return gridBody(t, []string{"Name", "Portion", "Quality", "Links"}, rows...)
}

func versionRow(title, quality, id string) []cell {
	return []cell{{value: title}, {value: "Full"}, {value: quality}, {value: "x", link: "https://pixeldrain.com/u/" + id}}
}

func TestSongs_VersionTaggedRowsBecomeOneSongWithTheBestVersionsLinks(t *testing.T) {
	songs := unreleasedSongs(t, versionSheet(t,
		versionRow("Sports Car [V3]", "CD Quality", "carV0003"),
		versionRow("Sports Car [V10]", "Lossless", "carV0010"),
		versionRow("Sports Car [V14]", "CD Quality", "carV0014"),
	))
	if len(songs) != 1 {
		t.Fatalf("songs = %+v, want one", songs)
	}
	s := songs[0]
	if s.Title != "Sports Car" || s.TrackerKey != "ken-carson:sports-car" || len(s.Versions) != 3 {
		t.Fatalf("song = %+v, want base title, base key and 3 versions", s)
	}
	if s.Quality != "Lossless" || !slices.Equal(s.AudioLinks, pixeldrain("carV0010")) || s.HasMedia == nil || !*s.HasMedia {
		t.Errorf("song = %+v, want V10's quality, links and media", s)
	}
	if s.Versions[0].Title != "Sports Car [V3]" || s.Versions[0].Label != "V3" {
		t.Errorf("first version = %+v, want the full sheet title and V3", s.Versions[0])
	}
}

func TestSongs_EqualQualityPrefersTheHighestVersionNumber(t *testing.T) {
	songs := unreleasedSongs(t, versionSheet(t,
		versionRow("Song [V9]", "High Quality", "songV009"),
		versionRow("Song [V12]", "high quality", "songV012"),
		versionRow("Song [V2]", "High Quality", "songV002"),
	))
	if len(songs) != 1 || !slices.Equal(songs[0].AudioLinks, pixeldrain("songV012")) {
		t.Errorf("songs = %+v, want one song with V12's links", songs)
	}
}

func TestSongs_DifferentBaseTitlesStayApartAndReleasedRowsAreNotVersions(t *testing.T) {
	unreleased := versionSheet(t,
		versionRow("Signs [V1]", "", "signsAAA"),
		versionRow("Siren Sounds [V1]", "", "sirenBBB"),
		versionRow("Filler [V1]", "", "fillCCC1"),
	)
	released := gridBody(t, releasedHeader(), offStreamingRows(releasedRow("Signs", "No", "https://pixeldrain.com/u/signsCCC"))...)
	songs := trackerSongs(t, tabsBody("Unreleased", "Released"), unreleased, released)
	signs := songTitled(t, songs, "Signs")
	if len(signs.Versions) != 1 || !signs.OffStreaming || len(signs.AudioLinks) != 2 {
		t.Errorf("signs = %+v, want one version, off_streaming and both links", signs)
	}
	if siren := songTitled(t, songs, "Siren Sounds"); siren.OffStreaming || len(siren.Versions) != 1 {
		t.Errorf("siren = %+v, want its own song, not off streaming", siren)
	}
}

func TestSongs_AVariantTitleIsNotFoldedIntoTheTaggedSong(t *testing.T) {
	songs := unreleasedSongs(t, versionSheet(t,
		versionRow("Song [V1]", "CD Quality", "songV001"),
		versionRow("Song (Instrumental)", "Lossless", "songInst"),
		versionRow("Other [V1]", "CD Quality", "otherV01"),
	))
	s := songTitled(t, songs, "Song")
	if !slices.Equal(s.AudioLinks, pixeldrain("songV001")) || len(s.Versions) != 1 {
		t.Errorf("song = %+v, want Song with only V1's links", s)
	}
	if v := songTitled(t, songs, "Song (Instrumental)"); !slices.Equal(v.AudioLinks, pixeldrain("songInst")) {
		t.Errorf("variant = %+v, want its own song", v)
	}
}
