package handler

import (
	"testing"

	"altune/go-api/internal/shared/logging"
)

func TestFilterByLevel(t *testing.T) {
	records := []logging.CapturedRecord{
		{Level: "DEBUG", Message: "d"},
		{Level: "INFO", Message: "i"},
		{Level: "WARN", Message: "w"},
		{Level: "ERROR", Message: "e"},
	}

	tests := []struct {
		name    string
		min     string
		wantLen int
	}{
		{"warn and above", "WARN", 2},
		{"error only", "ERROR", 1},
		{"info and above", "INFO", 3},
		{"debug keeps all", "DEBUG", 4},
		{"warn with offset suffix still ranks as warn", "WARN+2", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := len(filterByLevel(records, tt.min)); got != tt.wantLen {
				t.Errorf("filterByLevel(%q) len = %d, want %d", tt.min, got, tt.wantLen)
			}
		})
	}
}
