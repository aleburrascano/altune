package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestParseOptions(t *testing.T) {
	const run = "6f1c1f0e-6a52-4a3c-9d3e-0b1f2a3b4c5d"
	cases := []struct {
		name    string
		args    []string
		wantErr error
		check   func(options) bool
	}{
		{"dry run is the default", []string{"-report", "r.json"}, nil, func(o options) bool { return !o.apply && !o.undo && !o.status }},
		{"apply", []string{"-report", "r.json", "-apply"}, nil, func(o options) bool { return o.apply }},
		{"status", []string{"-status"}, nil, func(o options) bool { return o.status }},
		{"undo", []string{"-undo", run}, nil, func(o options) bool { return o.undo && o.undoRunID.String() == run }},
		{"apply without report", []string{"-apply"}, errApplyNoReport, nil},
		{"nothing", nil, errNoMode, nil},
		{"undo with apply", []string{"-undo", run, "-apply"}, errOneMode, nil},
		{"status with report", []string{"-status", "-report", "r.json"}, errOneMode, nil},
		{"status with apply", []string{"-status", "-apply"}, errOneMode, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseOptions(tc.args, io.Discard)
			if !errors.Is(err, tc.wantErr) || (tc.check != nil && !tc.check(got)) {
				t.Fatalf("got %+v, %v", got, err)
			}
		})
	}
}

func TestParseOptions_RejectsAMalformedRunID(t *testing.T) {
	if _, err := parseOptions([]string{"-undo", "nope"}, io.Discard); err == nil {
		t.Fatal("expected an error")
	}
}

func TestMaxPerRun(t *testing.T) {
	cases := []struct {
		raw     string
		want    int
		wantErr bool
	}{{"", 10, false}, {"3", 3, false}, {"0", 0, true}, {"-1", 0, true}, {"many", 0, true}}
	for _, tc := range cases {
		got, err := maxPerRun(func(string) string { return tc.raw })
		if got != tc.want || (err != nil) != tc.wantErr {
			t.Errorf("%q: got %d, %v", tc.raw, got, err)
		}
	}
}

func TestLoadPlan_ApplyRefusesAnAbortedReportFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "report.json")
	if err := os.WriteFile(path, []byte(`{"aborted":true,"findings":[`+fixFinding+`]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadPlan(path, true); !errors.Is(err, errReportAborted) {
		t.Fatalf("apply: %v", err)
	}
	if got, err := loadPlan(path, false); err != nil || len(got) != 1 {
		t.Fatalf("dry run: %+v, %v", got, err)
	}
}
