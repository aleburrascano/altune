package main

import (
	"bytes"
	"errors"
	"testing"
)

func TestParseOptions_ReportPathIsOptional(t *testing.T) {
	opts, err := parseOptions(nil, &bytes.Buffer{})
	if err != nil || opts.reportPath != "" {
		t.Fatalf("opts = %+v, err = %v", opts, err)
	}
	opts, err = parseOptions([]string{"-report", "out.json"}, &bytes.Buffer{})
	if err != nil || opts.reportPath != "out.json" {
		t.Fatalf("opts = %+v, err = %v", opts, err)
	}
}

func TestRun_WithoutADatabaseURLFailsBeforeAnyWork(t *testing.T) {
	err := run(options{}, func(string) string { return "" }, &bytes.Buffer{}, &bytes.Buffer{})
	if !errors.Is(err, errEmptyDatabaseURL) {
		t.Fatalf("err = %v", err)
	}
}
