package main

import "testing"

func envOf(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestLoadSettings_DefaultsWhenUnset(t *testing.T) {
	got, err := loadSettings(envOf(nil))
	if err != nil {
		t.Fatal(err)
	}
	if got.requestsPerSecond != 2 || got.workers != 2 || got.limit != 0 || got.reportPath != defaultReportPath {
		t.Fatalf("got %+v", got)
	}
}

func TestLoadSettings_ReadsEnv(t *testing.T) {
	got, err := loadSettings(envOf(map[string]string{
		"AUDIT_DATABASE_URL": "postgres://ro", "AUDIT_USER_ID": "u1", "AUDIT_LIMIT": "50",
		"AUDIT_DEEZER_RPS": "2.5", "AUDIT_WORKERS": "1", "AUDIT_REPORT_PATH": "/tmp/r.json",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got.databaseURL != "postgres://ro" || got.userID != "u1" || got.limit != 50 ||
		got.requestsPerSecond != 2.5 || got.workers != 1 || got.reportPath != "/tmp/r.json" {
		t.Fatalf("got %+v", got)
	}
}

func TestLoadSettings_RejectsBadNumbers(t *testing.T) {
	for _, env := range []map[string]string{
		{"AUDIT_DEEZER_RPS": "fast"}, {"AUDIT_DEEZER_RPS": "0"}, {"AUDIT_WORKERS": "-1"}, {"AUDIT_LIMIT": "x"},
	} {
		if _, err := loadSettings(envOf(env)); err == nil {
			t.Fatalf("expected an error for %v", env)
		}
	}
}
