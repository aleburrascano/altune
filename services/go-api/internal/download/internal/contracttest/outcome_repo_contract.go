package contracttest

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"context"
	"reflect"
	"testing"
)

type OutcomeRepoUnderTest interface {
	ports.OutcomeRecorder
	Recorded(t *testing.T, trackID string) []domain.AcquisitionOutcome
}

func OutcomeRepoContract(t *testing.T, newRepo func(t *testing.T) OutcomeRepoUnderTest) {
	t.Helper()
	ctx := context.Background()

	t.Run("an outcome reads back with every fact it was recorded with", func(t *testing.T) {
		repo := newRepo(t)
		want := domain.AcquisitionOutcome{
			TrackID:         contractIDs.NewString(),
			Outcome:         domain.JobSucceeded,
			Reason:          "stored",
			ElapsedMs:       4200,
			StageMs:         map[string]int64{"search": 800, "download": 3000},
			SourceHost:      "www.youtube.com",
			Source:          domain.SourceFormat{ID: "251", Codec: "opus", Kbps: 130},
			Stored:          domain.StoredFile{Bytes: 5_000_000, Codec: "mp3"},
			Kind:            domain.JobKindReplace,
			Run:             2,
			Provenance:      "catalog",
			Confidence:      0.85,
			SourceKey:       "yt:abcdefghijk",
			TargetSeconds:   204,
			MeasuredSeconds: 203.5,
		}

		if err := repo.Record(ctx, want); err != nil {
			t.Fatalf("Record: %v", err)
		}

		got := repo.Recorded(t, want.TrackID)
		if len(got) != 1 || !reflect.DeepEqual(got[0], want) {
			t.Errorf("Recorded = %+v, want exactly %+v", got, want)
		}
	})

	t.Run("an outcome with no optional facts reads back with them empty", func(t *testing.T) {
		repo := newRepo(t)
		want := domain.AcquisitionOutcome{
			TrackID:   contractIDs.NewString(),
			Outcome:   domain.JobFailed,
			Reason:    "no_match_found",
			ElapsedMs: 15,
			StageMs:   map[string]int64{},
		}

		if err := repo.Record(ctx, want); err != nil {
			t.Fatalf("Record: %v", err)
		}

		want.StageMs = nil
		got := repo.Recorded(t, want.TrackID)
		if len(got) != 1 || !reflect.DeepEqual(got[0], want) {
			t.Errorf("Recorded = %+v, want exactly %+v (an empty stage map is stored as none)", got, want)
		}
	})

	t.Run("outcomes of one track are all kept, in the order recorded", func(t *testing.T) {
		repo := newRepo(t)
		trackID := contractIDs.NewString()
		reasons := []string{"first", "second", "third"}

		for _, reason := range reasons {
			if err := repo.Record(ctx, domain.AcquisitionOutcome{TrackID: trackID, Outcome: domain.JobCancelled, Reason: reason}); err != nil {
				t.Fatalf("Record %s: %v", reason, err)
			}
		}

		var got []string
		for _, o := range repo.Recorded(t, trackID) {
			got = append(got, o.Reason)
		}
		if !reflect.DeepEqual(got, reasons) {
			t.Errorf("recorded reasons = %v, want %v", got, reasons)
		}
	})

	t.Run("a done context is refused and leaves nothing recorded", func(t *testing.T) {
		repo := newRepo(t)
		done, cancel := context.WithCancel(ctx)
		cancel()
		trackID := contractIDs.NewString()

		err := repo.Record(done, domain.AcquisitionOutcome{TrackID: trackID, Outcome: domain.JobSucceeded})

		if err == nil {
			t.Error("Record on a done context = nil, want an error")
		}
		if got := repo.Recorded(t, trackID); len(got) != 0 {
			t.Errorf("Recorded = %+v, want nothing", got)
		}
	})
}
