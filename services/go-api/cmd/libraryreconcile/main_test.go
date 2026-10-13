package main

import (
	"errors"
	"io"
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
		{"dry run is the default", []string{"-report", "r.json"}, nil, func(o options) bool { return !o.apply && !o.undo }},
		{"apply", []string{"-report", "r.json", "-apply"}, nil, func(o options) bool { return o.apply }},
		{"undo", []string{"-undo", run}, nil, func(o options) bool { return o.undo && o.undoRunID.String() == run }},
		{"apply without report", []string{"-apply"}, errApplyNoReport, nil},
		{"nothing", nil, errNoMode, nil},
		{"undo with apply", []string{"-undo", run, "-apply"}, errUndoWithReport, nil},
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
