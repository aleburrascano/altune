package main

import (
	"errors"
	"io"
	"testing"
)

func TestParseOptions(t *testing.T) {
	const runID = "6f1c1f0e-6a52-4a3c-9d3e-0b1f2a3b4c5d"
	cases := []struct {
		name    string
		args    []string
		wantErr error
		check   func(options) bool
	}{
		{"dry run is the default", nil, nil, func(o options) bool { return !o.apply && !o.undo }},
		{"apply", []string{"-apply", "-limit", "5"}, nil, func(o options) bool { return o.apply && o.limit == 5 }},
		{"undo", []string{"-undo", runID}, nil, func(o options) bool { return o.undo && o.undoRunID.String() == runID }},
		{"apply with undo", []string{"-apply", "-undo", runID}, errApplyAndUndo, nil},
		{"undo with a user filter", []string{"-undo", runID, "-user", "u"}, errUndoWithFilters, nil},
		{"negative limit", []string{"-limit", "-1"}, errNegativeLimit, nil},
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

func TestParseOptions_ReportBelongsToABindRun(t *testing.T) {
	got, err := parseOptions([]string{"-report", "plan.json"}, io.Discard)
	if err != nil || got.report != "plan.json" {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, err := parseOptions([]string{"-undo", "6f1c1f0e-6a52-4a3c-9d3e-0b1f2a3b4c5d", "-report", "plan.json"}, io.Discard); !errors.Is(err, errReportWithUndo) {
		t.Fatalf("err = %v", err)
	}
}

func TestParseOptions_RejectsAMalformedRunID(t *testing.T) {
	if _, err := parseOptions([]string{"-undo", "nope"}, io.Discard); err == nil {
		t.Fatal("expected an error")
	}
}

func TestParseOptions_PinsAreTheirOwnMode(t *testing.T) {
	got, err := parseOptions([]string{"-pins", "pins.json", "-apply"}, io.Discard)
	if err != nil || got.pins != "pins.json" || !got.apply {
		t.Fatalf("got %+v, %v", got, err)
	}
	for _, extra := range [][]string{{"-user", "u"}, {"-limit", "3"}, {"-report", "plan.json"}, {"-undo", "6f1c1f0e-6a52-4a3c-9d3e-0b1f2a3b4c5d"}} {
		if _, err := parseOptions(append([]string{"-pins", "pins.json"}, extra...), io.Discard); !errors.Is(err, errPinsWithBindFlag) {
			t.Errorf("-pins with %v: err = %v", extra, err)
		}
	}
}

func TestParseOptions_PinCatalogIsItsOwnMode(t *testing.T) {
	got, err := parseOptions([]string{"-pin-catalog", "-apply", "-user", "u"}, io.Discard)
	if err != nil || !got.pinCatalog || !got.apply || got.userID != "u" {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, err := parseOptions([]string{"-pin-catalog", "-undo", "6f1c1f0e-6a52-4a3c-9d3e-0b1f2a3b4c5d"}, io.Discard); !errors.Is(err, errCatalogPinWithUndo) {
		t.Fatalf("with undo: %v", err)
	}
	if _, err := parseOptions([]string{"-pin-catalog", "-report", "plan.json"}, io.Discard); !errors.Is(err, errCatalogPinWithReport) {
		t.Fatalf("with report: %v", err)
	}
}
