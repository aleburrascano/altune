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
