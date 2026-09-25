package service

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type mockStep struct {
	name         string
	executeErr   error
	executePanic any
	// cancelJob, when set, cancels the pipeline's own context from inside
	// Execute: the acquireTimeout firing or the scheduler shutting down
	// mid-pipeline.
	cancelJob      context.CancelFunc
	rollbackErr    error
	executed       bool
	rolledBack     bool
	rollbackCtxErr error
	executionLog   *[]string
}

func newMockStep(name string, executionLog *[]string) *mockStep {
	return &mockStep{name: name, executionLog: executionLog}
}

func (s *mockStep) Name() string { return s.name }

func (s *mockStep) Execute(_ context.Context, _ *AcquisitionContext) error {
	s.executed = true
	if s.executionLog != nil {
		*s.executionLog = append(*s.executionLog, "execute:"+s.name)
	}
	if s.cancelJob != nil {
		s.cancelJob()
	}
	if s.executePanic != nil {
		panic(s.executePanic)
	}
	return s.executeErr
}

func (s *mockStep) Rollback(ctx context.Context, _ *AcquisitionContext) error {
	s.rolledBack = true
	s.rollbackCtxErr = ctx.Err()
	if s.executionLog != nil {
		*s.executionLog = append(*s.executionLog, "rollback:"+s.name)
	}
	return s.rollbackErr
}

// mockStage adapts a mockStep into the typed slot of one pipeline stage.
type mockStage[In, Out any] struct{ *mockStep }

func (m mockStage[In, Out]) Execute(ctx context.Context, ac *AcquisitionContext, _ In) (Out, error) {
	var out Out
	return out, m.mockStep.Execute(ctx, ac)
}

var pipelineStageNames = []string{"search", "select", "download", "tag", "store", "update_track"}

// pipelineOf fills the pipeline's stages in order with steps. Stages past
// len(steps) get an unlogged pass-through mock named for the stage.
func pipelineOf(steps ...*mockStep) Pipeline {
	all := make([]*mockStep, len(pipelineStageNames))
	for i, name := range pipelineStageNames {
		if i < len(steps) {
			all[i] = steps[i]
		} else {
			all[i] = &mockStep{name: name}
		}
	}
	return Pipeline{
		search:      mockStage[pipelineStart, afterSearch]{all[0]},
		selectBest:  mockStage[afterSearch, afterSelect]{all[1]},
		download:    mockStage[afterSelect, afterDownload]{all[2]},
		tag:         mockStage[afterDownload, afterTag]{all[3]},
		store:       mockStage[afterTag, afterStore]{all[4]},
		updateTrack: mockStage[afterStore, afterUpdate]{all[5]},
	}
}

func TestRunPipeline(t *testing.T) {
	tests := []struct {
		name           string
		buildSteps     func(log *[]string) Pipeline
		wantErr        bool
		wantErrContain string
		wantLog        []string
	}{
		{
			name: "all steps succeed in order",
			buildSteps: func(log *[]string) Pipeline {
				return pipelineOf(
					newMockStep("search", log),
					newMockStep("select", log),
					newMockStep("download", log),
				)
			},
			wantLog: []string{
				"execute:search",
				"execute:select",
				"execute:download",
			},
		},
		{
			name: "step 3 fails triggers rollback of steps 1 and 2 in reverse",
			buildSteps: func(log *[]string) Pipeline {
				s1 := newMockStep("search", log)
				s2 := newMockStep("select", log)
				s3 := newMockStep("download", log)
				s3.executeErr = errors.New("download failed: connection reset")
				return pipelineOf(s1, s2, s3)
			},
			wantErr:        true,
			wantErrContain: "step download",
			wantLog: []string{
				"execute:search",
				"execute:select",
				"execute:download",
				"rollback:select",
				"rollback:search",
			},
		},
		{
			name: "all six stages succeed in order",
			buildSteps: func(log *[]string) Pipeline {
				steps := make([]*mockStep, len(pipelineStageNames))
				for i, name := range pipelineStageNames {
					steps[i] = newMockStep(name, log)
				}
				return pipelineOf(steps...)
			},
			wantLog: []string{
				"execute:search",
				"execute:select",
				"execute:download",
				"execute:tag",
				"execute:store",
				"execute:update_track",
			},
		},
		{
			name: "first step fails with no prior steps to rollback",
			buildSteps: func(log *[]string) Pipeline {
				s1 := newMockStep("search", log)
				s1.executeErr = errors.New("searcher unavailable")
				s2 := newMockStep("select", log)
				return pipelineOf(s1, s2)
			},
			wantErr:        true,
			wantErrContain: "step search",
			wantLog: []string{
				"execute:search",
			},
		},
		{
			name: "cancelled context triggers rollback of completed steps",
			buildSteps: func(log *[]string) Pipeline {
				return pipelineOf(
					newMockStep("search", log),
					newMockStep("select", log),
				)
			},
			wantErr:        true,
			wantErrContain: "pipeline cancelled",
			wantLog:        nil,
		},
		{
			name: "rollback error does not mask original step error",
			buildSteps: func(log *[]string) Pipeline {
				s1 := newMockStep("search", log)
				s1.rollbackErr = errors.New("rollback failed: file locked")
				s2 := newMockStep("select", log)
				s2.executeErr = errors.New("no candidates matched")
				return pipelineOf(s1, s2)
			},
			wantErr:        true,
			wantErrContain: "step select",
			wantLog: []string{
				"execute:search",
				"execute:select",
				"rollback:search",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "cancelled context triggers rollback of completed steps" {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()

				var log []string
				steps := tt.buildSteps(&log)
				ac := &AcquisitionContext{}

				err := RunPipeline(ctx, steps, ac)
				if err == nil {
					t.Fatal("expected error for cancelled context, got nil")
				}
				if !strings.Contains(err.Error(), "pipeline cancelled") {
					t.Errorf("error = %q, want it to contain %q", err.Error(), "pipeline cancelled")
				}
				return
			}

			ctx := context.Background()
			var log []string
			steps := tt.buildSteps(&log)
			ac := &AcquisitionContext{}

			err := RunPipeline(ctx, steps, ac)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErrContain)
				}
				if !strings.Contains(err.Error(), tt.wantErrContain) {
					t.Errorf("error = %q, want it to contain %q", err.Error(), tt.wantErrContain)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}

			if tt.wantLog != nil {
				if len(log) != len(tt.wantLog) {
					t.Fatalf("execution log length = %d, want %d\ngot:  %v\nwant: %v",
						len(log), len(tt.wantLog), log, tt.wantLog)
				}
				for i, entry := range tt.wantLog {
					if log[i] != entry {
						t.Errorf("execution log[%d] = %q, want %q\nfull log: %v", i, log[i], entry, log)
					}
				}
			}
		})
	}
}

func TestRunPipeline_SecondStepFails_OnlyFirstRolledBack(t *testing.T) {
	var log []string
	s1 := newMockStep("search", &log)
	s2 := newMockStep("select", &log)
	s2.executeErr = errors.New("no match")
	s3 := newMockStep("download", &log)

	steps := pipelineOf(s1, s2, s3)
	ac := &AcquisitionContext{}

	err := RunPipeline(context.Background(), steps, ac)

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	if s1.executed != true {
		t.Error("step 1 should have been executed")
	}
	if s2.executed != true {
		t.Error("step 2 should have been executed (it fails during execute)")
	}
	if s3.executed {
		t.Error("step 3 should NOT have been executed")
	}

	if s1.rolledBack != true {
		t.Error("step 1 should have been rolled back")
	}
	if s2.rolledBack {
		t.Error("step 2 should NOT have been rolled back (it failed, was never completed)")
	}
	if s3.rolledBack {
		t.Error("step 3 should NOT have been rolled back (never executed)")
	}
}

// A rollback runs precisely when the job's context is already done: the
// acquireTimeout fired, or the scheduler is shutting down. Compensations that
// inherit that cancellation fail their first call, so the audio stays in object
// storage with no reaper and the track never reverts.
func TestRunPipeline_RollbackRunsOnALiveContextAfterTheJobIsCancelled(t *testing.T) {
	cancellations := []struct {
		name       string
		executeErr error
	}{
		{name: "the job deadline fires between stages"},
		{name: "a stage fails as the job is cancelled", executeErr: errors.New("acquisition timed out")},
	}

	for _, tc := range cancellations {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			var log []string
			search := newMockStep("search", &log)
			selectBest := newMockStep("select", &log)
			download := newMockStep("download", &log)
			download.cancelJob = cancel
			download.executeErr = tc.executeErr

			err := RunPipeline(ctx, pipelineOf(search, selectBest, download), &AcquisitionContext{})
			if err == nil {
				t.Fatal("expected the cancelled pipeline to fail, got nil")
			}

			for _, step := range []*mockStep{search, selectBest} {
				if !step.rolledBack {
					t.Fatalf("step %q was not rolled back", step.name)
				}
				if step.rollbackCtxErr != nil {
					t.Errorf("step %q rolled back on a dead context (%v): its compensation cannot reach storage",
						step.name, step.rollbackCtxErr)
				}
			}
		})
	}
}

// TestRunPipeline_StepPanic_RollsBackAndReturnsStepError reproduces the defect
// where a panic mid-pipeline skipped rollback and propagated out, leaving the
// track stranded at Pending. RunPipeline must recover the panic, roll back the
// completed steps in reverse, and return it as a *StepError so acquire.go's
// normal failure path can mark the track Failed.
func TestRunPipeline_StepPanic_RollsBackAndReturnsStepError(t *testing.T) {
	var log []string
	s1 := newMockStep("search", &log)
	s2 := newMockStep("select", &log)
	s3 := newMockStep("download", &log)
	s3.executePanic = "boom: nil map write"

	steps := pipelineOf(s1, s2, s3)
	ac := &AcquisitionContext{}

	err := RunPipeline(context.Background(), steps, ac)

	if err == nil {
		t.Fatal("expected a returned error from a panicking step, got nil (panic propagated instead)")
	}

	var stepErr *StepError
	if !errors.As(err, &stepErr) {
		t.Fatalf("error = %T (%v), want *StepError", err, err)
	}
	if stepErr.Step != "download" {
		t.Errorf("StepError.Step = %q, want %q", stepErr.Step, "download")
	}

	if !s1.rolledBack {
		t.Error("step 1 (search) should have been rolled back after the panic")
	}
	if !s2.rolledBack {
		t.Error("step 2 (select) should have been rolled back after the panic")
	}
	if s3.rolledBack {
		t.Error("step 3 (download) should NOT have been rolled back (it panicked mid-execute, never completed)")
	}

	wantLog := []string{
		"execute:search",
		"execute:select",
		"execute:download",
		"rollback:select",
		"rollback:search",
	}
	if len(log) != len(wantLog) {
		t.Fatalf("execution log = %v, want %v", log, wantLog)
	}
	for i, entry := range wantLog {
		if log[i] != entry {
			t.Errorf("execution log[%d] = %q, want %q\nfull log: %v", i, log[i], entry, log)
		}
	}
}
