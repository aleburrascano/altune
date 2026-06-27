package service

import (
	"context"
	"testing"
)

type recordingReporter struct {
	stages []string
	srcURL string
}

func (r *recordingReporter) meta(string, string, string) {}
func (r *recordingReporter) stage(n string)              { r.stages = append(r.stages, n) }
func (r *recordingReporter) source(u string)             { r.srcURL = u }

type passStep struct{ n string }

func (s passStep) Name() string                                   { return s.n }
func (s passStep) Execute(context.Context, *AcquisitionContext) error  { return nil }
func (s passStep) Rollback(context.Context, *AcquisitionContext) error { return nil }

func TestRunPipeline_ReportsStageAndSource(t *testing.T) {
	rep := &recordingReporter{}
	ctx := withJobReporter(context.Background(), rep)
	ac := &AcquisitionContext{Track: TrackRef{ID: "t1"}, Selected: &Candidate{URL: "https://src/x"}}

	if err := RunPipeline(ctx, []Step{passStep{"search"}, passStep{"download"}}, ac); err != nil {
		t.Fatal(err)
	}
	if len(rep.stages) != 2 || rep.stages[0] != "search" || rep.stages[1] != "download" {
		t.Fatalf("stages = %v, want [search download]", rep.stages)
	}
	if rep.srcURL != "https://src/x" {
		t.Errorf("source = %q, want the selected candidate URL", rep.srcURL)
	}
}

func TestRunPipeline_NoReporter_IsNoOp(t *testing.T) {
	// No reporter in context → jobReporterFrom returns the no-op; must not panic.
	ac := &AcquisitionContext{Track: TrackRef{ID: "t1"}}
	if err := RunPipeline(context.Background(), []Step{passStep{"store"}}, ac); err != nil {
		t.Fatal(err)
	}
}
