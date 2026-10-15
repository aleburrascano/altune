package main

import (
	"errors"
	"io"
	"testing"
	"time"
)

func TestParseOptions(t *testing.T) {
	const run = "6f1c1f0e-6a52-4a3c-9d3e-0b1f2a3b4c5d"
	cases := []struct {
		name    string
		args    []string
		wantErr error
		check   func(options) bool
	}{
		{"dry run is the default", nil, nil, func(o options) bool { return !o.apply && !o.undo }},
		{"apply", []string{"-apply"}, nil, func(o options) bool { return o.apply }},
		{"limit", []string{"-limit", "5"}, nil, func(o options) bool { return o.limit == 5 }},
		{"undo", []string{"-undo", run}, nil, func(o options) bool { return o.undo && o.undoRunID.String() == run }},
		{"undo with apply", []string{"-undo", run, "-apply"}, errApplyAndUndo, nil},
		{"undo with limit", []string{"-undo", run, "-limit", "3"}, errUndoWithFilters, nil},
		{"undo with user", []string{"-undo", run, "-user", "u"}, errUndoWithFilters, nil},
		{"user", []string{"-user", "u"}, nil, func(o options) bool { return o.userID == "u" }},
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

func TestMinIntervalFromEnv(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		want    time.Duration
		wantErr error
	}{
		{"unset uses the default", "", defaultMinInterval, nil},
		{"a duration", "500ms", 500 * time.Millisecond, nil},
		{"zero", "0s", 0, errBadMinInterval},
		{"garbage", "soon", 0, errBadMinInterval},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := minIntervalFromEnv(func(string) string { return tc.value })
			if got != tc.want || !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v, %v", got, err)
			}
		})
	}
}
