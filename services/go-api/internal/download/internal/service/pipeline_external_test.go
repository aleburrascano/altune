package service_test

import (
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/download/internal/service"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"testing"
)

type panickingTagger struct{}

func (panickingTagger) Tag(context.Context, string, ports.TrackTags) error {
	panic("tagger exploded")
}

func TestExecute_APanickingStepIsLoggedAndReportedAsThatStep(t *testing.T) {
	ring := logging.CaptureRing(t, "error")

	_, err := acquireStaged(t, pendingBlindingLights(), freshAudioSource{}, noStages(taggedBy(panickingTagger{})))

	var stepErr *service.StepError
	if !errors.As(err, &stepErr) || stepErr.Step != "tag" {
		t.Fatalf("Execute = %v, want a StepError naming the tag step", err)
	}
	var panics []logging.CapturedRecord
	for _, r := range ring.Snapshot() {
		if r.Message == "acquisition.pipeline_panic" {
			panics = append(panics, r)
		}
	}
	if len(panics) != 1 {
		t.Fatalf("logged %d acquisition.pipeline_panic records, want exactly 1", len(panics))
	}
	if got := panics[0].Attrs["step"]; got != "tag" {
		t.Errorf("panic record step = %q, want %q", got, "tag")
	}
	if got := panics[0].Attrs["panic"]; got == "" {
		t.Error("panic record has no panic value")
	}
}
