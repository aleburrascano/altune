package logging_test

import (
	"altune/go-api/internal/shared/logging"
	"os"
	"regexp"
	"testing"
)

func TestKeyConstants_NameTheWireKeys(t *testing.T) {
	want := map[string]string{
		logging.KeyCorrelationID: "corr_id",
		logging.KeyError:         "error",
		logging.KeyMethod:        "method",
		logging.KeyPath:          "path",
		logging.KeyRoute:         "route",
		logging.KeyUserID:        "user_id",
		logging.KeyStatus:        "status",
		logging.KeyCode:          "code",
		logging.KeyTrackID:       "track_id",
		logging.KeyDurationMS:    "duration_ms",
		logging.KeyBytes:         "bytes",
		logging.KeyStack:         "stack",
		logging.KeyPanic:         "panic",
		logging.KeyJob:           "job",
	}
	rules, err := os.ReadFile("../../../_rules/logkeys.rules")
	if err != nil {
		t.Fatalf("read the raw-key lint rule: %v", err)
	}
	rejected := map[string]bool{}
	for _, found := range regexp.MustCompile("`\\$f\\(\\$\\*_, \"([a-z_]+)\", \\$\\*_\\)`").FindAllSubmatch(rules, -1) {
		rejected[string(found[1])] = true
	}
	if len(rejected) == 0 {
		t.Fatal("_rules/logkeys.rules rejects no key literal")
	}
	for got, wire := range want {
		if got != wire {
			t.Errorf("key constant = %q, want %q", got, wire)
		}
		if !rejected[wire] {
			t.Errorf("key %q has a constant but _rules/logkeys.rules does not reject its raw literal", wire)
		}
	}
}
