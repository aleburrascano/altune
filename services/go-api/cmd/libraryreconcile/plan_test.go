package main

import (
	"os"
	"strings"
	"testing"
)

func text(s string) *string { return &s }

const testYear = 2026

func TestReadPlan_GroupsFixesPerTrackAndIgnoresFindingsWithoutFix(t *testing.T) {
	report := `{"findings":[
	  {"track_id":"t1","category":"no_isrc"},
	  {"track_id":"t1","category":"year_fillable","fix":{"user_id":"u1","version":3,"field":"year","old_value":null,"new_value":"2019"}},
	  {"track_id":"t1","category":"album_artist_fillable","fix":{"user_id":"u1","version":3,"field":"album_artist","old_value":null,"new_value":"Daniel Caesar"}},
	  {"track_id":"t2","category":"wrong_song","fix":{"user_id":"u2","version":0,"field":"isrc","old_value":"USWRONG00001","new_value":null}},
	  {"track_id":"t1","category":"year_fillable","fix":{"user_id":"u1","version":3,"field":"year","old_value":null,"new_value":"2020"}}
	]}`
	plans, err := readPlan(strings.NewReader(report), testYear)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 2 || plans[0].TrackID != "t1" || plans[0].Version != 3 || len(plans[0].Changes) != 2 {
		t.Fatalf("got %+v", plans)
	}
	if *plans[0].Changes[0].NewValue != "2019" || plans[0].Changes[1].Field != fieldAlbumArtist {
		t.Fatalf("got %+v", plans[0].Changes)
	}
	if !plans[1].Changes[0].clearsWrongISRC() || *plans[1].Changes[0].AuditedOld != "USWRONG00001" {
		t.Fatalf("got %+v", plans[1].Changes)
	}
}

func TestReadPlan_RejectsFixesItCannotTrust(t *testing.T) {
	cases := map[string]string{
		"unknown field":      `{"findings":[{"track_id":"t1","fix":{"user_id":"u","version":1,"field":"title","new_value":"x"}}]}`,
		"year without value": `{"findings":[{"track_id":"t1","fix":{"user_id":"u","version":1,"field":"year","new_value":null}}]}`,
		"no user":            `{"findings":[{"track_id":"t1","fix":{"version":1,"field":"year","new_value":"2019"}}]}`,
		"not json":           `nope`,
		"year too early":     `{"findings":[{"track_id":"t1","fix":{"user_id":"u","version":1,"field":"year","new_value":"1850"}}]}`,
		"year too late":      `{"findings":[{"track_id":"t1","fix":{"user_id":"u","version":1,"field":"year","new_value":"2031"}}]}`,
		"year not a number":  `{"findings":[{"track_id":"t1","fix":{"user_id":"u","version":1,"field":"year","new_value":"20x9"}}]}`,
		"blank album artist": `{"findings":[{"track_id":"t1","fix":{"user_id":"u","version":1,"field":"album_artist","new_value":"  "}}]}`,
		"isrc fill":          `{"findings":[{"track_id":"t1","fix":{"user_id":"u","version":1,"field":"isrc","new_value":"USFOUND00001"}}]}`,
		"isrc clear of junk": `{"findings":[{"track_id":"t1","fix":{"user_id":"u","version":1,"field":"isrc","old_value":"X","new_value":null}}]}`,
		"user disagrees": `{"findings":[
		  {"track_id":"t1","fix":{"user_id":"u1","version":1,"field":"year","new_value":"2019"}},
		  {"track_id":"t1","fix":{"user_id":"u2","version":1,"field":"album_artist","new_value":"A"}}]}`,
		"version disagrees": `{"findings":[
		  {"track_id":"t1","fix":{"user_id":"u1","version":1,"field":"year","new_value":"2019"}},
		  {"track_id":"t1","fix":{"user_id":"u1","version":2,"field":"album_artist","new_value":"A"}}]}`,
	}
	for name, report := range cases {
		t.Run(name, func(t *testing.T) {
			if plans, err := readPlan(strings.NewReader(report), testYear); err == nil {
				t.Fatalf("expected an error, got %+v", plans)
			}
		})
	}
}

func TestReadPlan_ReadsTheReportLibraryauditWrites(t *testing.T) {
	file, err := os.Open("../libraryaudit/testdata/report.json")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	plans, err := readPlan(file, testYear)
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 2 || len(plans[0].Changes) != 2 || plans[0].Version != 7 ||
		*plans[0].Changes[0].NewValue != "2019" || plans[1].Changes[0].Field != fieldISRC || *plans[1].Changes[0].AuditedOld != "USWRONG00001" {
		t.Fatalf("got %+v", plans)
	}
}

func TestDecide(t *testing.T) {
	fill := func(f field) plannedChange { return plannedChange{Field: f, NewValue: text("v")} }
	clear := plannedChange{Field: fieldISRC, AuditedOld: text("USWRONG00001")}
	cases := []struct {
		name    string
		change  plannedChange
		current *string
		want    verdict
	}{
		{"null year fills", fill(fieldYear), nil, verdictApplied},
		{"zero year fills", fill(fieldYear), text("0"), verdictApplied},
		{"set year is kept", fill(fieldYear), text("1999"), verdictSkippedNotEmpty},
		{"blank album artist fills", fill(fieldAlbumArtist), text("  "), verdictApplied},
		{"set album artist is kept", fill(fieldAlbumArtist), text("Someone"), verdictSkippedNotEmpty},
		{"wrong isrc still seen is cleared", clear, text("USWRONG00001"), verdictApplied},
		{"isrc changed since audit is kept", clear, text("USOTHER00001"), verdictSkippedNotEmpty},
		{"isrc already gone is left", clear, nil, verdictSkippedNotEmpty},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := decide(tc.change, tc.current); got != tc.want {
				t.Fatalf("decide = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestReadPlan_AcceptsAnISRCFillOnlyWhenBothCatalogsAgreed(t *testing.T) {
	fill := func(sources string) string {
		return `{"findings":[{"track_id":"t1","fix":{"user_id":"u","version":1,"field":"isrc","old_value":null,"new_value":"USFOUND00001"` + sources + `}}]}`
	}
	accepted := map[string]string{"both sources": fill(`,"sources":["deezer","musicbrainz"]`)}
	for name, report := range accepted {
		plans, err := readPlan(strings.NewReader(report), testYear)
		if err != nil || len(plans) != 1 || plans[0].Changes[0].clearsWrongISRC() || *plans[0].Changes[0].NewValue != "USFOUND00001" {
			t.Fatalf("%s: err=%v plans=%+v", name, err, plans)
		}
	}
	refused := map[string]string{
		"no sources":           fill(``),
		"deezer only":          fill(`,"sources":["deezer"]`),
		"musicbrainz only":     fill(`,"sources":["musicbrainz"]`),
		"unrelated sources":    fill(`,"sources":["deezer","itunes"]`),
		"lower case isrc":      `{"findings":[{"track_id":"t1","fix":{"user_id":"u","version":1,"field":"isrc","new_value":"usfound00001","sources":["deezer","musicbrainz"]}}]}`,
		"short isrc":           `{"findings":[{"track_id":"t1","fix":{"user_id":"u","version":1,"field":"isrc","new_value":"US1","sources":["deezer","musicbrainz"]}}]}`,
		"replaces an old isrc": `{"findings":[{"track_id":"t1","fix":{"user_id":"u","version":1,"field":"isrc","old_value":"USOLD0000001","new_value":"USFOUND00001","sources":["deezer","musicbrainz"]}}]}`,
	}
	for name, report := range refused {
		if plans, err := readPlan(strings.NewReader(report), testYear); err == nil {
			t.Fatalf("%s: expected an error, got %+v", name, plans)
		}
	}
}

func TestDecide_ISRCFillOnlyIntoAnEmptyISRC(t *testing.T) {
	fill := plannedChange{Field: fieldISRC, NewValue: text("USFOUND00001")}
	if got := decide(fill, nil); got != verdictApplied {
		t.Fatalf("empty isrc: %s", got)
	}
	if got := decide(fill, text("USOTHER00001")); got != verdictSkippedNotEmpty {
		t.Fatalf("set isrc: %s", got)
	}
}

func TestReadPlan_AcceptsTheCatalogFieldsOnlyWithUsableValues(t *testing.T) {
	fix := func(field, value string) string {
		return `{"findings":[{"track_id":"t1","fix":{"user_id":"u","version":1,"field":"` + field + `","old_value":null,"new_value":` + value + `}}]}`
	}
	accepted := map[string]string{
		"track number": fix("track_number", `"4"`),
		"genre":        fix("genre", `"R&B"`),
		"catalog len":  fix("catalog_duration_seconds", `"236.5"`),
	}
	for name, report := range accepted {
		if plans, err := readPlan(strings.NewReader(report), testYear); err != nil || len(plans) != 1 {
			t.Fatalf("%s: err=%v plans=%+v", name, err, plans)
		}
	}
	refused := map[string]string{
		"track number zero":      fix("track_number", `"0"`),
		"track number negative":  fix("track_number", `"-2"`),
		"track number not whole": fix("track_number", `"4.5"`),
		"track number huge":      fix("track_number", `"5000"`),
		"track number null":      fix("track_number", `null`),
		"genre blank":            fix("genre", `"  "`),
		"genre null":             fix("genre", `null`),
		"length zero":            fix("catalog_duration_seconds", `"0"`),
		"length negative":        fix("catalog_duration_seconds", `"-3"`),
		"length not a number":    fix("catalog_duration_seconds", `"long"`),
		"length NaN":             fix("catalog_duration_seconds", `"NaN"`),
		"length a day and more":  fix("catalog_duration_seconds", `"90000"`),
		"length null":            fix("catalog_duration_seconds", `null`),
	}
	for name, report := range refused {
		if plans, err := readPlan(strings.NewReader(report), testYear); err == nil {
			t.Fatalf("%s: expected an error, got %+v", name, plans)
		}
	}
}

func TestDecide_CatalogFieldsFillOnlyWhenEmpty(t *testing.T) {
	cases := []struct {
		name    string
		field   field
		current *string
		want    verdict
	}{
		{"null track number", fieldTrackNumber, nil, verdictApplied},
		{"zero track number", fieldTrackNumber, text("0"), verdictApplied},
		{"set track number", fieldTrackNumber, text("3"), verdictSkippedNotEmpty},
		{"null genre", fieldGenre, nil, verdictApplied},
		{"blank genre", fieldGenre, text(" "), verdictApplied},
		{"set genre", fieldGenre, text("Pop"), verdictSkippedNotEmpty},
		{"null length", fieldCatalogLen, nil, verdictApplied},
		{"zero length", fieldCatalogLen, text("0"), verdictApplied},
		{"set length", fieldCatalogLen, text("236"), verdictSkippedNotEmpty},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := decide(plannedChange{Field: tc.field, NewValue: text("1")}, tc.current); got != tc.want {
				t.Fatalf("decide = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestReadPlan_AcceptsAnArtworkFillOrReplacementOnlyWithAnHTTPSNewUrlAndNeverABlankOldOne(t *testing.T) {
	fix := func(old, value string) string {
		return `{"findings":[{"track_id":"t1","fix":{"user_id":"u","version":1,"field":"artwork_url","old_value":` + old + `,"new_value":` + value + `}}]}`
	}
	if plans, err := readPlan(strings.NewReader(fix(`"https://lib/a.jpg"`, `"https://cdn/b.jpg"`)), testYear); err != nil || len(plans) != 1 {
		t.Fatalf("err=%v plans=%+v", err, plans)
	}
	if plans, err := readPlan(strings.NewReader(fix(`null`, `"https://cdn/b.jpg"`)), testYear); err != nil || len(plans) != 1 || plans[0].Changes[0].Mode != modeFill || plans[0].Changes[0].AuditedOld != nil {
		t.Fatalf("a missing old url is a fill: err=%v plans=%+v", err, plans)
	}
	refused := map[string]string{
		"blank old url": fix(`" "`, `"https://cdn/b.jpg"`),
		"no new url":    fix(`"https://lib/a.jpg"`, `null`),
		"http":          fix(`"https://lib/a.jpg"`, `"http://cdn/b.jpg"`),
		"not a url":     fix(`"https://lib/a.jpg"`, `"cover.jpg"`),
		"no host":       fix(`"https://lib/a.jpg"`, `"https:///b.jpg"`),
	}
	for name, report := range refused {
		if plans, err := readPlan(strings.NewReader(report), testYear); err == nil {
			t.Fatalf("%s: expected an error, got %+v", name, plans)
		}
	}
}

func TestDecide_ArtworkIsReplacedOnlyWhileItIsStillWhatTheAuditSaw(t *testing.T) {
	replace := plannedChange{Field: fieldArtworkURL, AuditedOld: text("https://lib/a.jpg"), NewValue: text("https://cdn/b.jpg")}
	cases := []struct {
		name    string
		current *string
		want    verdict
	}{
		{"still the audited cover", text("https://lib/a.jpg"), verdictApplied},
		{"changed since the audit", text("https://lib/other.jpg"), verdictSkippedNotEmpty},
		{"cover removed since the audit", nil, verdictSkippedNotEmpty},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := decide(replace, tc.current); got != tc.want {
				t.Fatalf("decide = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestDecide_ClearsAWrongCatalogLengthOnlyWhileItIsTheValueTheAuditSaw(t *testing.T) {
	change := plannedChange{Field: fieldCatalogLen, AuditedOld: text("287")}
	if got := decide(change, text("287")); got != verdictApplied {
		t.Errorf("same value: got %s", got)
	}
	if got := decide(change, text("226")); got != verdictSkippedNotEmpty {
		t.Errorf("changed since the audit: got %s", got)
	}
}

func TestCheckValues_ACatalogLengthClearNeedsTheOldValue(t *testing.T) {
	if err := checkValues(fieldCatalogLen, &reportFix{OldValue: text("287")}, 2026); err != nil {
		t.Errorf("clear with old value: %v", err)
	}
	if err := checkValues(fieldCatalogLen, &reportFix{}, 2026); err == nil {
		t.Error("clear without old value was accepted")
	}
}

func TestReadPlan_CorrectModeNeedsTheValueTheDeriveSawAndAcceptsAnISRCCorrectionOnlyWithBothValues(t *testing.T) {
	cases := map[string]string{
		"no old value":    `{"findings":[{"track_id":"t1","fix":{"user_id":"u","version":1,"field":"year","mode":"correct","new_value":"1984"}}]}`,
		"blank old value": `{"findings":[{"track_id":"t1","fix":{"user_id":"u","version":1,"field":"genre","mode":"correct","old_value":" ","new_value":"Pop"}}]}`,
		"no new value":    `{"findings":[{"track_id":"t1","fix":{"user_id":"u","version":1,"field":"year","mode":"correct","old_value":"2024","new_value":null}}]}`,
		"isrc no old":     `{"findings":[{"track_id":"t1","fix":{"user_id":"u","version":1,"field":"isrc","mode":"correct","new_value":"USAAA0000002"}}]}`,
		"isrc no new":     `{"findings":[{"track_id":"t1","fix":{"user_id":"u","version":1,"field":"isrc","mode":"correct","old_value":"USAAA0000001","new_value":null}}]}`,
		"year too early":  `{"findings":[{"track_id":"t1","fix":{"user_id":"u","version":1,"field":"year","mode":"correct","old_value":"2024","new_value":"1850"}}]}`,
		"unknown mode":    `{"findings":[{"track_id":"t1","fix":{"user_id":"u","version":1,"field":"year","mode":"overwrite","old_value":"2024","new_value":"1984"}}]}`,
	}
	for name, report := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := readPlan(strings.NewReader(report), testYear); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestReadPlan_ReadsAnISRCCorrection(t *testing.T) {
	report := `{"findings":[{"track_id":"t1","fix":{"user_id":"u","version":1,"field":"isrc","mode":"correct","old_value":"USAAA0000001","new_value":"USAAA0000002","sources":["deezer","musicbrainz"]}}]}`
	plans, err := readPlan(strings.NewReader(report), testYear)
	if err != nil || len(plans) != 1 || plans[0].Changes[0].Mode != modeCorrect || plans[0].Changes[0].Field != "isrc" || *plans[0].Changes[0].NewValue != "USAAA0000002" {
		t.Fatalf("got %+v, %v", plans, err)
	}
}

func TestReadPlan_ReadsACorrectModeFix(t *testing.T) {
	report := `{"findings":[{"track_id":"t1","fix":{"user_id":"u","version":2,"field":"year","mode":"correct","old_value":"2024","new_value":"1984"}}]}`
	plans, err := readPlan(strings.NewReader(report), testYear)
	if err != nil || len(plans) != 1 || plans[0].Changes[0].Mode != modeCorrect {
		t.Fatalf("got %+v, %v", plans, err)
	}
}

func TestDecide_CorrectModeReplacesOnlyTheValueTheDeriveSaw(t *testing.T) {
	correct := plannedChange{Mode: modeCorrect, Field: fieldYear, AuditedOld: text("2024"), NewValue: text("1984")}
	cases := []struct {
		name    string
		current *string
		want    verdict
	}{
		{"still the audited value", text("2024"), verdictApplied},
		{"edited since", text("2001"), verdictSkippedNotEmpty},
		{"already the new value", text("1984"), verdictSkippedNotEmpty},
		{"emptied since", nil, verdictSkippedNotEmpty},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := decide(correct, tc.current); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}

func identityFix(field, mode, old, value, group string) string {
	return `{"track_id":"t1","fix":{"user_id":"u","version":2,"field":"` + field + `","mode":"` + mode + `","old_value":` + old + `,"new_value":` + value + `,"group":"` + group + `"}}`
}

func TestReadPlan_ReadsAnAlbumMoveGroupAndRefusesWhatCouldBreakTheDedupKeyContract(t *testing.T) {
	album := identityFix("album", "correct", `"Now 14"`, `"Simple Pleasures"`, "g1")
	genre := identityFix("genre", "correct", `"Pop"`, `"Jazz"`, "g1")
	plans, err := readPlan(strings.NewReader(`{"findings":[`+album+`,`+genre+`]}`), testYear)
	if err != nil || len(plans) != 1 || len(plans[0].Changes) != 2 || plans[0].Changes[0].Group != "g1" || plans[0].Changes[1].Group != "g1" {
		t.Fatalf("got %+v, %v", plans, err)
	}
	refused := map[string]string{
		"a dedup key":             identityFix("dedup_key", "correct", `"a|b|c"`, `"d|e|f"`, ""),
		"a filled title":          identityFix("title", "fill", `null`, `"Song"`, ""),
		"a blank album":           identityFix("album", "correct", `"Now 14"`, `" "`, ""),
		"a NUL in the artist":     identityFix("artist", "correct", `"A"`, `"B\u0000"`, ""),
		"a padded title":          identityFix("title", "correct", `"A"`, `" Song  Two"`, ""),
		"a full-width artist":     identityFix("artist", "correct", `"A"`, `"Ｂ"`, ""),
		"an artist outside group": identityFix("artist", "correct", `"A"`, `"B"`, "") + "," + album,
	}
	for name, fix := range refused {
		if plans, err := readPlan(strings.NewReader(`{"findings":[`+fix+`]}`), testYear); err == nil {
			t.Errorf("%s: expected an error, got %+v", name, plans)
		}
	}
}

func TestReadPlan_AcceptsTheDeriveFixesThatFillACoverFillAnAlbumAndCorrectAnISRC(t *testing.T) {
	fix := func(field, mode, old, value, sources string) string {
		return `{"findings":[{"track_id":"t1","fix":{"user_id":"u","version":1,"field":"` + field + `","mode":"` + mode +
			`","old_value":` + old + `,"new_value":` + value + `,"sources":` + sources + `}}]}`
	}
	accepted := map[string]string{
		"cover fill":     fix("artwork_url", "fill", `null`, `"https://cdn/b.jpg"`, `[]`),
		"album fill":     fix("album", "fill", `null`, `"Make It Big"`, `[]`),
		"isrc fill":      fix("isrc", "fill", `null`, `"GBAYE8400002"`, `["track_identity"]`),
		"isrc correct":   fix("isrc", "correct", `"GBAYE8400001"`, `"GBAYE8400002"`, `["track_identity"]`),
		"cover replaced": fix("artwork_url", "correct", `"https://cdn/a.jpg"`, `"https://cdn/b.jpg"`, `[]`),
	}
	for name, report := range accepted {
		if plans, err := readPlan(strings.NewReader(report), testYear); err != nil || len(plans) != 1 {
			t.Errorf("%s: err=%v plans=%+v", name, err, plans)
		}
	}
	if _, err := readPlan(strings.NewReader(fix("isrc", "correct", `"GBAYE8400001"`, `"GBAYE8400002"`, `[]`)), testYear); err == nil {
		t.Error("an isrc correction with no proof in sources was accepted")
	}
}
