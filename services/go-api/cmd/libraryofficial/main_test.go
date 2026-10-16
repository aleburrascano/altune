package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
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

func TestParseOptions_BadRunID(t *testing.T) {
	if _, err := parseOptions([]string{"-undo", "nope"}, io.Discard); err == nil {
		t.Fatal("want an error")
	}
}

func TestPacingFromEnv(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		want    pacing
		wantErr error
	}{
		{"defaults", nil, pacing{concurrency: 2, minInterval: 5 * time.Second}, nil},
		{"both set", map[string]string{concurrencyVarName: "4", minIntervalVarName: "250ms"}, pacing{concurrency: 4, minInterval: 250 * time.Millisecond}, nil},
		{"no spacing at all", map[string]string{minIntervalVarName: "0s"}, pacing{concurrency: 2}, nil},
		{"zero workers", map[string]string{concurrencyVarName: "0"}, pacing{}, errBadConcurrencyEnv},
		{"too many workers", map[string]string{concurrencyVarName: "17"}, pacing{}, errBadConcurrencyEnv},
		{"workers that are not a number", map[string]string{concurrencyVarName: "many"}, pacing{}, errBadConcurrencyEnv},
		{"interval that is not a duration", map[string]string{minIntervalVarName: "fast"}, pacing{}, errBadMinIntervalEnv},
		{"negative interval", map[string]string{minIntervalVarName: "-1s"}, pacing{}, errBadMinIntervalEnv},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := pacingFromEnv(func(key string) string { return tc.env[key] })
			if !errors.Is(err, tc.wantErr) || got != tc.want {
				t.Fatalf("got %+v, %v", got, err)
			}
		})
	}
}

func TestBudgetedClient_LogsTheSharedPace(t *testing.T) {
	server := miniredis.RunT(t)
	env := func(key string) string {
		return map[string]string{"REDIS_URL": "redis://" + server.Addr()}[key]
	}
	var out bytes.Buffer
	client, err := budgetedClient(context.Background(), env, &out)
	if err != nil {
		t.Fatal(err)
	}
	if client == nil {
		t.Fatal("nil client without an error")
	}
	if got := out.String(); !strings.HasPrefix(got, "shared InnerTube budget: one call per ") {
		t.Fatalf("log = %q, want the shared pace line", got)
	}
}
