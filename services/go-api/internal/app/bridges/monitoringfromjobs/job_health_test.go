package monitoringfromjobs_test

import (
	"altune/go-api/internal/app/bridges/monitoringfromjobs"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"errors"
	"slices"
	"testing"
)

func TestJobHealth_ReadsTheFailureStreakAndTheKillSwitchOfEachNamedJob(t *testing.T) {
	var registry background.JobRegistry
	names := []string{"a job", "another job"}
	health := monitoringfromjobs.NewJobHealth(names, func(name string) monitoringfromjobs.Control { return registry.Control(name) })

	if !slices.Equal(health.Alertable(), names) {
		t.Errorf("Alertable = %v, want %v", health.Alertable(), names)
	}
	for range 3 {
		registry.Control("a job").Record(clock.Now(), errors.New("boom"))
	}
	registry.SetEnabled("another job", false)

	if got := health.State("a job"); got.ConsecutiveFailures != 3 || got.Disabled {
		t.Errorf("a job = %+v, want three failures and enabled", got)
	}
	if got := health.State("another job"); got.ConsecutiveFailures != 0 || !got.Disabled {
		t.Errorf("another job = %+v, want no failures and disabled", got)
	}
}

func TestJobHealth_RegistersEveryNamedJobWhenBuilt(t *testing.T) {
	var registry background.JobRegistry

	monitoringfromjobs.NewJobHealth([]string{"quiet job"}, func(name string) monitoringfromjobs.Control { return registry.Control(name) })

	if got := registry.Health(); len(got) != 1 || got[0].Name != "quiet job" {
		t.Errorf("registered jobs = %+v, want the quiet job listed before it ever runs", got)
	}
}
