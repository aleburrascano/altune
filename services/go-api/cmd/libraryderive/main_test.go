package main

import (
	"errors"
	"io"
	"testing"
	"time"
)

func TestParseOptions(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr error
		want    options
	}{
		{"defaults", nil, nil, options{reportPath: defaultReportPath}},
		{"filters and report", []string{"-user", "u1", "-limit", "5", "-report", "/tmp/r.json"}, nil, options{userID: "u1", limit: 5, reportPath: "/tmp/r.json"}},
		{"negative limit", []string{"-limit", "-1"}, errNegativeLimit, options{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseOptions(tc.args, io.Discard)
			if !errors.Is(err, tc.wantErr) || got != tc.want {
				t.Fatalf("got %+v, %v", got, err)
			}
		})
	}
}

func TestOpenReadOnlyPool_RefusesAnEmptyDatabaseURL(t *testing.T) {
	if _, err := openReadOnlyPool(t.Context(), ""); !errors.Is(err, errEmptyDatabaseURL) {
		t.Fatalf("err = %v", err)
	}
}

func TestParseOptions_Since(t *testing.T) {
	got, err := parseOptions([]string{"-since", "2026-10-14T09:30:00Z"}, io.Discard)
	if err != nil || !got.since.Equal(time.Date(2026, 10, 14, 9, 30, 0, 0, time.UTC)) {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, err := parseOptions([]string{"-since", "yesterday"}, io.Discard); err == nil {
		t.Fatal("a non-RFC3339 -since was accepted")
	}
}

func TestThrottlePolicyFromEnv(t *testing.T) {
	env := func(vars map[string]string) func(string) string {
		return func(name string) string { return vars[name] }
	}
	cases := []struct {
		name    string
		vars    map[string]string
		want    throttlePolicy
		wantErr error
	}{
		{"defaults ride out minutes of throttling", nil, throttlePolicy{Attempts: 10, MaxWait: time.Minute}, nil},
		{"both set", map[string]string{"LIBRARYDERIVE_THROTTLE_ATTEMPTS": "3", "LIBRARYDERIVE_THROTTLE_MAX_WAIT": "90s"}, throttlePolicy{Attempts: 3, MaxWait: 90 * time.Second}, nil},
		{"zero attempts", map[string]string{"LIBRARYDERIVE_THROTTLE_ATTEMPTS": "0"}, throttlePolicy{}, errBadThrottleAttempts},
		{"unparsable wait", map[string]string{"LIBRARYDERIVE_THROTTLE_MAX_WAIT": "soon"}, throttlePolicy{}, errBadThrottleMaxWait},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := throttlePolicyFromEnv(env(tc.vars))
			if !errors.Is(err, tc.wantErr) || got != tc.want {
				t.Fatalf("got %+v, %v", got, err)
			}
		})
	}
}
