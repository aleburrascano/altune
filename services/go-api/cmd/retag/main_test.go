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
		{"dry run over every ready track is the default", nil, nil, func(o options) bool { return !o.apply && !o.sel.changedOnly() && o.limit == 0 }},
		{"apply with limit", []string{"-apply", "-limit", "5"}, nil, func(o options) bool { return o.apply && o.limit == 5 }},
		{"by run", []string{"-run", run}, nil, func(o options) bool { return o.sel.runID.String() == run && o.sel.changedOnly() }},
		{"since", []string{"-since", "2026-10-13T00:00:00Z"}, nil, func(o options) bool { return o.sel.since.Year() == 2026 && o.sel.changedOnly() }},
		{"run and since", []string{"-run", run, "-since", "2026-10-13T00:00:00Z"}, errRunAndSince, nil},
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

func TestParseOptions_RejectsMalformedValues(t *testing.T) {
	for _, args := range [][]string{{"-run", "nope"}, {"-since", "yesterday"}} {
		if _, err := parseOptions(args, io.Discard); err == nil {
			t.Errorf("parseOptions(%v) = nil error", args)
		}
	}
}

func TestConcurrencyFrom(t *testing.T) {
	cases := []struct {
		raw     string
		want    int
		wantErr bool
	}{{"", defaultConcurrency, false}, {"4", 4, false}, {"0", 0, true}, {"17", 0, true}, {"x", 0, true}}
	for _, tc := range cases {
		got, err := concurrencyFrom(tc.raw)
		if got != tc.want || (err != nil) != tc.wantErr {
			t.Errorf("concurrencyFrom(%q) = %d, %v", tc.raw, got, err)
		}
	}
}
