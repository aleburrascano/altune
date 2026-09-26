package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// reportModeRequiredEnv sets the config env vars run() needs to reach mode
// dispatch, deliberately leaving DATABASE_URL unset: the report mode must
// never need a database connection (#2806).
func reportModeRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("SUPABASE_PROJECT_URL", "https://example.supabase.co")
	t.Setenv("SUPABASE_JWT_JWKS_URL", "https://example.supabase.co/auth/v1/.well-known/jwks.json")
}

// TestRun_ReportModeNeverTouchesTheDatabase pins run()'s mode dispatch: -mode
// report must reach runReport without ever building a database pool, so a
// manual nightly report run succeeds with no DATABASE_URL configured (#2806).
// Without the dispatch, run() falls through to database.NewPool, which fails
// with a distinct "DATABASE_URL not set" error instead of the report error.
func TestRun_ReportModeNeverTouchesTheDatabase(t *testing.T) {
	reportModeRequiredEnv(t)

	err := run(options{mode: "report", reportsDir: ""})

	if err == nil {
		t.Fatal("run() = nil error, want the report-mode reports-dir error")
	}
	const want = "report mode needs -reports pointing at a directory of metrics-*.json"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("run() error = %q, want it to contain %q (a database error means -mode report reached database.NewPool)", err.Error(), want)
	}
}

func TestClampSinceDays_AboveWindowClampsWithNotice(t *testing.T) {
	var notice bytes.Buffer

	clamped := clampSinceDays(&notice, aggregateRetentionDays+909)

	if clamped != aggregateRetentionDays {
		t.Errorf("clamped = %d, want %d (the retention ceiling)", clamped, aggregateRetentionDays)
	}
	if !strings.Contains(notice.String(), fmt.Sprintf("clamping to %d", aggregateRetentionDays)) {
		t.Errorf("notice did not announce the clamp:\n%s", notice.String())
	}
}

func TestClampSinceDays_AtWindowIsUntouchedAndSilent(t *testing.T) {
	var notice bytes.Buffer

	clamped := clampSinceDays(&notice, aggregateRetentionDays)

	if clamped != aggregateRetentionDays {
		t.Errorf("clamped = %d, want %d unchanged", clamped, aggregateRetentionDays)
	}
	if notice.Len() != 0 {
		t.Errorf("a within-window value must not print a notice, got:\n%s", notice.String())
	}
}

func TestClampSinceDays_WithinWindowIsUntouchedAndSilent(t *testing.T) {
	var notice bytes.Buffer

	clamped := clampSinceDays(&notice, 30)

	if clamped != 30 {
		t.Errorf("clamped = %d, want 30 unchanged", clamped)
	}
	if notice.Len() != 0 {
		t.Errorf("a within-window value must not print a notice, got:\n%s", notice.String())
	}
}
