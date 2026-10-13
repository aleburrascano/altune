package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSummarize_CountsEachCategory(t *testing.T) {
	outcome := auditOutcome{Total: 5, Checked: 3, Findings: []finding{
		{Category: categoryLengthShorter},
		{Category: categoryLengthShorter},
		{Category: categoryLengthLonger},
		{Category: categoryCoverURLDiffers},
		{Category: categoryWrongSong},
	}}
	got := summarize(outcome)
	if got.ReadyTracks != 5 || got.Checked != 3 || got.LengthShorter != 2 || got.LengthLonger != 1 ||
		got.CoverURLDiffers != 1 || got.WrongSong != 1 || got.NoISRC != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestWriteJSON_RoundTripsWithEmptyFindingsAsList(t *testing.T) {
	var out bytes.Buffer
	result := buildReport(auditOutcome{Total: 1, Checked: 1}, time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC))
	if err := writeJSON(&out, result); err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Summary  map[string]int `json:"summary"`
		Findings []any          `json:"findings"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Findings == nil || decoded.Summary["checked"] != 1 || decoded.Summary["ready_tracks"] != 1 {
		t.Fatalf("got %s", out.String())
	}
}

func TestWriteSummary_NamesEveryCountedCategory(t *testing.T) {
	var out bytes.Buffer
	writeSummary(&out, report{Summary: summary{ReadyTracks: 2249, Checked: 1958, NoISRC: 278, UnknownISRC: 13, WrongSong: 8, LengthShorter: 30, LengthLonger: 27}})
	for _, want := range []string{"2249 ready", "1958", "278", "13", "different song", "shorter", "longer", "year fillable", "cover"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in\n%s", want, out.String())
		}
	}
}

func TestBuildReport_AbortedRunSaysWhyAndKeepsTheSummary(t *testing.T) {
	result := buildReport(auditOutcome{Total: 10, Checked: 3, AbortReason: "catalog is blocking requests: HTTP 403"}, time.Now())
	var out bytes.Buffer
	if err := writeJSON(&out, result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"aborted": true`) || !strings.Contains(out.String(), "HTTP 403") || result.Summary.Checked != 3 {
		t.Fatalf("got %s", out.String())
	}
	var text bytes.Buffer
	writeSummary(&text, result)
	if !strings.Contains(text.String(), "ABORTED") {
		t.Fatalf("got %s", text.String())
	}
}

func goldenOutcome() auditOutcome {
	base := libraryTrack{ID: "t1", UserID: "u1", Version: 7, Title: "Valentina", Artist: "Daniel Caesar"}
	year := about(base, categoryYearFillable, "2019")
	year.Fix = fixing(base, "year", "", "2019")
	albumArtist := about(base, categoryAlbumArtistFillable, "Daniel Caesar")
	albumArtist.Fix = fixing(base, "album_artist", "", "Daniel Caesar")
	wrongTrack := libraryTrack{ID: "t2", UserID: "u2", Version: 3, Title: "TEC", Artist: "Lil Tecca", ISRC: "USWRONG00001"}
	wrong := about(wrongTrack, categoryWrongSong, "catalog says Lil Tecca - Ransom")
	wrong.Fix = fixing(wrongTrack, "isrc", wrongTrack.ISRC, "")
	searched := about(libraryTrack{ID: "t3", UserID: "u3", Version: 1, Title: "Song", Artist: "Artist"}, categoryISRCFillable, "USFOUND00001")
	return auditOutcome{Total: 3, Checked: 2, Findings: []finding{year, albumArtist, wrong, searched}}
}

func TestWriteJSON_MatchesTheReportFileLibraryreconcileReads(t *testing.T) {
	var out bytes.Buffer
	if err := writeJSON(&out, buildReport(goldenOutcome(), time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC))); err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/report.json")
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != string(want) {
		t.Fatalf("got:\n%s", out.String())
	}
}

func TestAuditTrack_AnISRCFoundBySearchIsReportedWithoutAFix(t *testing.T) {
	cat := fakeCatalog{
		search: []catalogTrack{{ID: 2, Title: "Valentina", Artist: "Daniel Caesar", DurationSeconds: 236}},
		byID:   map[int64]catalogTrack{2: {ID: 2, ISRC: "USFOUND"}},
	}
	found := findingOf(audited(cat, fakeCovers{}, libTrack()).Findings, categoryISRCFillable)
	if found.Detail != "USFOUND" || found.Fix != nil {
		t.Fatalf("got %+v", found)
	}
}
