package jobs_test

import (
	"altune/go-api/internal/app/jobs"
	"reflect"
	"strings"
	"testing"
)

var everyName = []jobs.Name{
	jobs.OrphanedAudioReconcile,
	jobs.BehavioralCorpusRefresh,
	jobs.DiscographyEventPrune,
	jobs.VocabularyRefresh,
	jobs.BehavioralRankingRefresh,
	jobs.DeletedIdentityErasure,
	jobs.AcquisitionSourceCanary,
	jobs.StreamRecovery,
	jobs.AcquisitionPrune,
	jobs.AcquisitionUpgrade,
	jobs.UnreleasedRefresh,
}

func TestNames_WireIdentifiersUnchanged(t *testing.T) {
	want := map[jobs.Name]string{
		jobs.EvalMeter:                "eval meter",
		jobs.AlertMonitor:             "alert monitor",
		jobs.OrphanedAudioReconcile:   "orphaned audio reconcile",
		jobs.BehavioralCorpusRefresh:  "behavioral corpus refresh",
		jobs.DiscographyEventPrune:    "discography event prune",
		jobs.VocabularyRefresh:        "vocabulary refresh",
		jobs.BehavioralRankingRefresh: "behavioral ranking refresh",
		jobs.DeletedIdentityErasure:   "deleted identity erasure",
		jobs.AcquisitionSourceCanary:  "acquisition source canary",
		jobs.StreamRecovery:           "stream recovery",
		jobs.AcquisitionPrune:         "acquisition prune",
		jobs.AcquisitionUpgrade:       "acquisition upgrade",
		jobs.UnreleasedRefresh:        "unreleased refresh",
	}
	for name, wire := range want {
		if string(name) != wire {
			t.Errorf("job %q goes on the wire as %q, want %q", wire, name, wire)
		}
	}
}

func TestDisableAtStartup_AcceptsEveryJobButTheAlwaysOnErasure(t *testing.T) {
	for _, name := range everyName {
		if name == jobs.DeletedIdentityErasure {
			continue
		}
		var r jobs.Runner
		if err := r.DisableAtStartup(t.Context(), []string{string(name)}); err != nil {
			t.Errorf("DISABLED_JOBS=%q refused: %v", name, err)
			continue
		}
		if h := findHealth(t, &r, string(name)); h.Enabled {
			t.Errorf("job %q still enabled after DISABLED_JOBS named it", name)
		}
	}
}

func TestAlertable_PinnedInOrder(t *testing.T) {
	want := []jobs.Name{
		jobs.OrphanedAudioReconcile,
		jobs.BehavioralCorpusRefresh,
		jobs.DiscographyEventPrune,
		jobs.VocabularyRefresh,
		jobs.BehavioralRankingRefresh,
		jobs.DeletedIdentityErasure,
		jobs.AcquisitionSourceCanary,
		jobs.AcquisitionPrune,
		jobs.AcquisitionUpgrade,
		jobs.UnreleasedRefresh,
	}
	if got := jobs.Alertable(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Alertable() = %v, want %v", got, want)
	}
}

func TestAlertable_OnlyStreamRecoveryIsExcluded(t *testing.T) {
	alertable := map[jobs.Name]bool{}
	for _, name := range jobs.Alertable() {
		alertable[name] = true
	}
	for _, name := range everyName {
		if excluded := name == jobs.StreamRecovery; alertable[name] == excluded {
			t.Errorf("job %q alertable=%v excluded=%v: exactly one must hold", name, alertable[name], excluded)
		}
	}
}

func TestDisableAtStartup_DisablesEachNamedJob(t *testing.T) {
	var r jobs.Runner

	if err := r.DisableAtStartup(t.Context(), []string{"acquisition prune", "stream recovery"}); err != nil {
		t.Fatalf("DisableAtStartup: %v", err)
	}

	for _, name := range []string{"acquisition prune", "stream recovery"} {
		if h := findHealth(t, &r, name); h.Enabled {
			t.Errorf("job %q enabled, want disabled via DISABLED_JOBS", name)
		}
	}
}

func TestDisableAtStartup_LogsEachJobItTurnsOffOnce(t *testing.T) {
	var r jobs.Runner
	logged := captureLogs(t)

	if err := r.DisableAtStartup(t.Context(), []string{"acquisition prune", "acquisition prune"}); err != nil {
		t.Fatalf("DisableAtStartup: %v", err)
	}

	if got := strings.Count(logged.String(), `msg="job disabled at startup" job="acquisition prune"`); got != 1 {
		t.Errorf("logged the startup disable %d times, want once: %q", got, logged.String())
	}
}

func TestDisableAtStartup_AnUnknownOrAlwaysOnJobFailsStartup(t *testing.T) {
	for _, raw := range []string{"nope", "alert monitor", "eval meter", "deleted identity erasure"} {
		var r jobs.Runner
		err := r.DisableAtStartup(t.Context(), []string{raw})
		if err == nil || !strings.Contains(err.Error(), "DISABLED_JOBS: unknown job") || !strings.Contains(err.Error(), raw) {
			t.Errorf("DISABLED_JOBS=%q gave %v, want the unknown-job startup error naming it", raw, err)
		}
	}
}
