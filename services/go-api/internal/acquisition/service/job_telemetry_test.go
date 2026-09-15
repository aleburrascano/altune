package service

import (
	"context"
	"testing"

	"altune/go-api/internal/acquisition/ports"
)

type recordingReporter struct {
	stages []string
	srcURL string
	prov   string
}

func (r *recordingReporter) meta(string, string, string) {}
func (r *recordingReporter) stage(n string)              { r.stages = append(r.stages, n) }
func (r *recordingReporter) source(u string)             { r.srcURL = u }
func (r *recordingReporter) provenance(p string)         { r.prov = p }

func TestRunPipeline_ReportsStageAndSource(t *testing.T) {
	rep := &recordingReporter{}
	ctx := withJobReporter(context.Background(), rep)
	ac := &AcquisitionContext{Track: TrackRef{ID: "t1"}, Selected: &ports.AudioCandidate{URL: "https://src/x"}}

	if err := RunPipeline(ctx, pipelineOf(), ac); err != nil {
		t.Fatal(err)
	}
	assertStepOrder(t, rep.stages, pipelineStageNames)
	if rep.srcURL != "https://src/x" {
		t.Errorf("source = %q, want the selected candidate URL", rep.srcURL)
	}
}

func TestRunPipeline_NoReporter_IsNoOp(t *testing.T) {
	ac := &AcquisitionContext{Track: TrackRef{ID: "t1"}}
	if err := RunPipeline(context.Background(), pipelineOf(), ac); err != nil {
		t.Fatal(err)
	}
}
