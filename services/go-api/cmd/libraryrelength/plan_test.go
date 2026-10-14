package main

import (
	"errors"
	"strings"
	"testing"
)

const fixFinding = `{"track_id":"t1","artist":"Prince","title":"1999","category":"length_version_fix",
"length_plan":{"action":"fix_isrc","user_id":"u1","version":3,"old_isrc":"USALBUM","new_isrc":"USRADIO","file_seconds":226,"catalog_seconds":225}}`

const reacquireFinding = `{"track_id":"t2","artist":"Drake","title":"In My Feelings","category":"length_reacquire",
"length_plan":{"action":"reacquire","user_id":"u1","version":5,"old_isrc":"USDRAKE","file_seconds":480,"catalog_seconds":218}}`

func TestReadPlan_TakesLengthPlansAndIgnoresOtherFindings(t *testing.T) {
	report := `{"findings":[{"track_id":"t9","category":"length_shorter"},` + fixFinding + `,` + reacquireFinding + `,` + fixFinding + `]}`
	got, err := readPlan(strings.NewReader(report))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Action != actionFixISRC || got[0].NewISRC != "USRADIO" || got[0].Version != 3 || got[0].Artist != "Prince" ||
		got[1].Action != actionReacquire || got[1].CatalogSeconds != 218 || got[1].OldISRC != "USDRAKE" {
		t.Fatalf("got %+v", got)
	}
}

func TestReadPlan_RejectsPlansItCannotTrust(t *testing.T) {
	plan := func(body string) string {
		return `{"findings":[{"track_id":"t1","length_plan":{"user_id":"u","version":1,"old_isrc":"A",` + body + `}}]}`
	}
	cases := map[string]string{
		"unknown action":              plan(`"action":"delete","file_seconds":100,"catalog_seconds":200`),
		"fix without new isrc":        plan(`"action":"fix_isrc","file_seconds":100,"catalog_seconds":100`),
		"fix to the same isrc":        plan(`"action":"fix_isrc","new_isrc":"a","file_seconds":100,"catalog_seconds":100`),
		"fix whose length misses":     plan(`"action":"fix_isrc","new_isrc":"B","file_seconds":100,"catalog_seconds":200`),
		"reacquire with new isrc":     plan(`"action":"reacquire","new_isrc":"B","file_seconds":100,"catalog_seconds":200`),
		"reacquire of a fitting file": plan(`"action":"reacquire","file_seconds":100,"catalog_seconds":105`),
		"no lengths":                  plan(`"action":"reacquire"`),
		"not json":                    `nope`,
		"no user":                     `{"findings":[{"track_id":"t1","length_plan":{"action":"reacquire","old_isrc":"A","file_seconds":100,"catalog_seconds":200}}]}`,
		"no old isrc":                 `{"findings":[{"track_id":"t1","length_plan":{"action":"reacquire","user_id":"u","file_seconds":100,"catalog_seconds":200}}]}`,
	}
	for name, report := range cases {
		t.Run(name, func(t *testing.T) {
			if got, err := readPlan(strings.NewReader(report)); err == nil {
				t.Fatalf("expected an error, got %+v", got)
			}
		})
	}
}

func TestReadApplyPlan_RefusesAnAbortedReportButDryRunStillReadsIt(t *testing.T) {
	aborted := `{"aborted":true,"abort_reason":"deezer blocked","findings":[` + fixFinding + `]}`
	if got, err := readApplyPlan(strings.NewReader(aborted)); !errors.Is(err, errReportAborted) || got != nil {
		t.Fatalf("apply: got %+v, %v", got, err)
	}
	if got, err := readPlan(strings.NewReader(aborted)); err != nil || len(got) != 1 {
		t.Fatalf("dry run: got %+v, %v", got, err)
	}
}

func TestReadApplyPlan_AcceptsReportsWrittenBeforeTheAbortedFieldExisted(t *testing.T) {
	for name, report := range map[string]string{
		"no aborted key":    `{"findings":[` + fixFinding + `]}`,
		"aborted is false":  `{"aborted":false,"findings":[` + fixFinding + `]}`,
		"aborted with none": `{"aborted":false,"findings":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := readApplyPlan(strings.NewReader(report)); err != nil {
				t.Fatal(err)
			}
		})
	}
}
