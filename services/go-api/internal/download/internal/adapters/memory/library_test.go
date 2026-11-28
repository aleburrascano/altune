package memory_test

import (
	"altune/go-api/internal/download/internal/adapters/memory"
	"altune/go-api/internal/download/internal/domain"
	"encoding/json"
	"testing"
)

func TestLibrary_EvidenceAcceptedNeedsValidJSONAndAConfidenceInRange(t *testing.T) {
	cases := []struct {
		name string
		c    domain.Completion
		want bool
	}{
		{"valid evidence", domain.Completion{Confidence: 0.5, Evidence: json.RawMessage(`{"k":1}`)}, true},
		{"no evidence", domain.Completion{Confidence: 0.5}, false},
		{"confidence out of range", domain.Completion{Confidence: 1.5, Evidence: json.RawMessage(`{"k":1}`)}, false},
		{"evidence that is not JSON", domain.Completion{Confidence: 0.5, Evidence: json.RawMessage(`{`)}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := memory.NewLibrary().EvidenceAccepted(tc.c); got != tc.want {
				t.Errorf("EvidenceAccepted = %v, want %v", got, tc.want)
			}
		})
	}
}
