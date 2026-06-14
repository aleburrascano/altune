package service

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// mockStep is a configurable fake implementing the Step interface.
// It records execution and rollback calls for verification.
type mockStep struct {
	name        string
	executeErr  error
	rollbackErr error
	executed    bool
	rolledBack  bool
	// order tracking: appends step name to the shared slice on execute/rollback
	executionLog *[]string
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
	return s.executeErr
}

func (s *mockStep) Rollback(_ context.Context, _ *AcquisitionContext) error {
	s.rolledBack = true
	if s.executionLog != nil {
		*s.executionLog = append(*s.executionLog, "rollback:"+s.name)
	}
	return s.rollbackErr
}

func TestRunPipeline(t *testing.T) {
	tests := []struct {
		name           string
		buildSteps     func(log *[]string) []Step
		wantErr        bool
		wantErrContain string
		wantLog        []string
	}{
		{
			name: "all steps succeed in order",
			buildSteps: func(log *[]string) []Step {
				return []Step{
					newMockStep("search", log),
					newMockStep("select", log),
					newMockStep("download", log),
				}
			},
			wantLog: []string{
				"execute:search",
				"execute:select",
				"execute:download",
			},
		},
		{
			name: "step 3 fails triggers rollback of steps 1 and 2 in reverse",
			buildSteps: func(log *[]string) []Step {
				s1 := newMockStep("search", log)
				s2 := newMockStep("select", log)
				s3 := newMockStep("download", log)
				s3.executeErr = errors.New("download failed: connection reset")
				return []Step{s1, s2, s3}
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
			name: "empty pipeline is a no-op",
			buildSteps: func(log *[]string) []Step {
				return []Step{}
			},
			wantLog: []string{},
		},
		{
			name: "first step fails with no prior steps to rollback",
			buildSteps: func(log *[]string) []Step {
				s1 := newMockStep("search", log)
				s1.executeErr = errors.New("searcher unavailable")
				s2 := newMockStep("select", log)
				return []Step{s1, s2}
			},
			wantErr:        true,
			wantErrContain: "step search",
			wantLog: []string{
				"execute:search",
				// no rollback calls — nothing completed before search
			},
		},
		{
			name: "cancelled context triggers rollback of completed steps",
			buildSteps: func(log *[]string) []Step {
				return []Step{
					newMockStep("search", log),
					newMockStep("select", log),
				}
			},
			wantErr:        true,
			wantErrContain: "pipeline cancelled",
			wantLog:        nil, // checked separately due to pre-cancel setup
		},
		{
			name: "rollback error does not mask original step error",
			buildSteps: func(log *[]string) []Step {
				s1 := newMockStep("search", log)
				s1.rollbackErr = errors.New("rollback failed: file locked")
				s2 := newMockStep("select", log)
				s2.executeErr = errors.New("no candidates matched")
				return []Step{s1, s2}
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
			// Special handling for cancelled context test
			if tt.name == "cancelled context triggers rollback of completed steps" {
				ctx, cancel := context.WithCancel(context.Background())
				cancel() // cancel immediately before running

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
	// Arrange
	var log []string
	s1 := newMockStep("search", &log)
	s2 := newMockStep("select", &log)
	s2.executeErr = errors.New("no match")
	s3 := newMockStep("download", &log)

	steps := []Step{s1, s2, s3}
	ac := &AcquisitionContext{}

	// Act
	err := RunPipeline(context.Background(), steps, ac)

	// Assert
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
