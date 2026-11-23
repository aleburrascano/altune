package app_test

import (
	"altune/go-api/internal/app"
	"altune/go-api/internal/app/jobs"
	"altune/go-api/internal/shared/runloop"
	"strings"
	"testing"
	"time"
)

func jobHealth(t *testing.T, assembly *app.Assembly, name jobs.Name) runloop.JobHealth {
	t.Helper()
	for _, h := range assembly.Jobs().Health() {
		if h.Name == string(name) {
			return h
		}
	}
	t.Fatalf("job %q not in the assembly's job health", name)
	return runloop.JobHealth{}
}

func TestStartupSwitches_AcquisitionPausedWithoutAnyScheduler(t *testing.T) {
	cfg := jwksConfig(t)
	cfg.AcquisitionPaused = true

	assembleLogging(t, cfg)
}

func TestStartupSwitches_DisabledJobsDisableEachNamedJob(t *testing.T) {
	cfg := jwksConfig(t)
	cfg.DisabledJobs = []string{string(jobs.AcquisitionPrune), string(jobs.StreamRecovery), string(jobs.UnreleasedRefresh)}

	assembly := assembleLogging(t, cfg).assembly

	for _, name := range cfg.DisabledJobs {
		if jobHealth(t, assembly, jobs.Name(name)).Enabled {
			t.Errorf("job %q enabled, want disabled via DISABLED_JOBS", name)
		}
	}
}

func TestStartupSwitches_AnUnknownDisabledJobFailsAssembly(t *testing.T) {
	cfg := jwksConfig(t)
	cfg.DisabledJobs = []string{"nope"}

	_, err := app.Assemble(t.Context(), cfg, app.Handles{Pool: unreachablePool(t), Transport: refuseEgress{}})

	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("Assemble = %v, want an error naming %q", err, "nope")
	}
}

func TestStartupSwitches_ADisabledRankingRefreshSkipsInsteadOfRunning(t *testing.T) {
	cfg := jwksConfig(t)
	cfg.BehavioralRankingEnabled = true
	cfg.DisabledJobs = []string{string(jobs.BehavioralRankingRefresh)}
	assembly := assembleLogging(t, cfg).assembly

	deadline := time.Now().Add(3 * time.Second)
	h := jobHealth(t, assembly, jobs.BehavioralRankingRefresh)
	for h.Skipped == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		h = jobHealth(t, assembly, jobs.BehavioralRankingRefresh)
	}

	if h.Skipped == 0 || h.Failures != 0 || !h.LastSuccess.IsZero() {
		t.Errorf("ranking refresh health = %+v, want a skipped pass and no run against the store", h)
	}
}
