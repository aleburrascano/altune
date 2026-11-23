package housekeeping_test

import (
	"altune/go-api/internal/app/housekeeping"
	"altune/go-api/internal/discovery"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type failingLabelStore struct{}

func (failingLabelStore) BehavioralLabels(context.Context, time.Time) ([]discovery.BehavioralLabel, error) {
	return nil, errors.New("boom")
}

func TestMaterializeBehavioralCorpus_AFailedBuildLogsTheOldTextAndFails(t *testing.T) {
	var err error
	logged := captureLogs(t, func() {
		err = housekeeping.MaterializeBehavioralCorpus(failingLabelStore{}, "/tmp/corpus.json")(context.Background())
	})

	if err == nil {
		t.Error("run error = nil, want the build failure so the job counts it")
	}
	want := `level=WARN msg="behavioral corpus materialize failed" error="build behavioral corpus: boom"` + "\n"
	if !strings.Contains(logged, want) {
		t.Errorf("want %q in\n%s", want, logged)
	}
}

type sinceRecordingStore struct{ since *time.Time }

func (s sinceRecordingStore) BehavioralLabels(_ context.Context, since time.Time) ([]discovery.BehavioralLabel, error) {
	*s.since = since
	return nil, nil
}

func TestMaterializeBehavioralCorpus_WritesTheLastThirtyDaysToThePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corpus.json")
	var since time.Time
	var err error
	logged := captureLogs(t, func() {
		err = housekeeping.MaterializeBehavioralCorpus(sinceRecordingStore{since: &since}, path)(context.Background())
	})

	if err != nil {
		t.Fatalf("run error = %v, want nil", err)
	}
	if want := time.Now().UTC().Add(-30 * 24 * time.Hour); since.Before(want.Add(-time.Minute)) || since.After(want) {
		t.Errorf("labels read since %v, want 30 days before now (%v)", since, want)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Errorf("corpus not written: %v", statErr)
	}
	if want := `level=INFO msg="behavioral corpus materialized" path=` + path + "\n"; logged != want {
		t.Errorf("logged %q, want %q", logged, want)
	}
}
