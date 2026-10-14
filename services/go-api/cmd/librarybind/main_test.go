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

func TestParseOptions_RejectsAMalformedRunID(t *testing.T) {
	if _, err := parseOptions([]string{"-undo", "nope"}, io.Discard); err == nil {
		t.Fatal("expected an error")
	}
}
