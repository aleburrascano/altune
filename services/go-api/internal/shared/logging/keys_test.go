package logging_test

import (
	"altune/go-api/internal/shared/logging"
	"testing"
)

func TestKeyConstants_NameTheWireKeys(t *testing.T) {
	want := map[string]string{
		logging.KeyCorrelationID: "corr_id",
		logging.KeyTraceID:       "trace_id",
		logging.KeyError:         "error",
		logging.KeyMethod:        "method",
		logging.KeyPath:          "path",
		logging.KeyRoute:         "route",
		logging.KeyUserID:        "user_id",
		logging.KeyStatus:        "status",
		logging.KeyCode:          "code",
		logging.KeyDuration:      "duration",
		logging.KeyBytes:         "bytes",
		logging.KeyPanic:         "panic",
		logging.KeyStack:         "stack",
	}
	for got, wire := range want {
		if got != wire {
			t.Errorf("key constant = %q, want %q", got, wire)
		}
	}
}
