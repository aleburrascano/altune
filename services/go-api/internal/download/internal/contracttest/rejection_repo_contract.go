package contracttest

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/ids"
	"context"
	"reflect"
	"testing"
	"time"
)

type RejectionRepoUnderTest interface {
	ports.RejectionStore
}

func RejectionRepoContract(t *testing.T, newRepo func(t *testing.T) RejectionRepoUnderTest) {
	t.Helper()
	ctx := context.Background()
	anHourAgo := func() time.Time { return time.Now().Add(-time.Hour) }
	basis := domain.RejectionBasis{ExpectedSeconds: 204, ReferenceMBID: "11111111-2222-3333-4444-555555555555"}
	record := func(trackID ids.TrackId, key string, b domain.RejectionBasis) domain.CandidateRejectionRecord {
		return domain.CandidateRejectionRecord{TrackID: trackID.String(), SourceKey: key, Reason: "duration", Detail: "off", Basis: b}
	}
	active := func(t *testing.T, repo RejectionRepoUnderTest, trackID ids.TrackId, since time.Time) []domain.PriorRejection {
		t.Helper()
		got, err := repo.Active(ctx, trackID, since)
		if err != nil {
			t.Fatalf("Active: %v", err)
		}
		return got
	}

	t.Run("a recorded rejection is active with the basis it was judged on", func(t *testing.T) {
		repo, trackID := newRepo(t), ids.NewTrackId()

		if err := repo.Record(ctx, []domain.CandidateRejectionRecord{record(trackID, "yt:aaa", basis)}); err != nil {
			t.Fatalf("Record: %v", err)
		}

		want := []domain.PriorRejection{{SourceKey: "yt:aaa", Basis: &basis}}
		if got := active(t, repo, trackID, anHourAgo()); !reflect.DeepEqual(got, want) {
			t.Errorf("Active = %+v, want %+v", got, want)
		}
	})

	t.Run("active rejections belong to their own track", func(t *testing.T) {
		repo, mine, other := newRepo(t), ids.NewTrackId(), ids.NewTrackId()

		if err := repo.Record(ctx, []domain.CandidateRejectionRecord{record(other, "yt:aaa", basis)}); err != nil {
			t.Fatalf("Record: %v", err)
		}

		if got := active(t, repo, mine, anHourAgo()); len(got) != 0 {
			t.Errorf("Active for another track = %+v, want none", got)
		}
	})

	t.Run("a rejection older than the asked window is not active", func(t *testing.T) {
		repo, trackID := newRepo(t), ids.NewTrackId()
		if err := repo.Record(ctx, []domain.CandidateRejectionRecord{record(trackID, "yt:aaa", basis)}); err != nil {
			t.Fatalf("Record: %v", err)
		}

		if got := active(t, repo, trackID, time.Now().Add(time.Hour)); len(got) != 0 {
			t.Errorf("Active since an hour from now = %+v, want none", got)
		}
	})

	t.Run("rejecting a source again replaces what was recorded for it", func(t *testing.T) {
		repo, trackID := newRepo(t), ids.NewTrackId()
		later := domain.RejectionBasis{ExpectedSeconds: 150, ReferenceMBID: "another"}

		for _, b := range []domain.RejectionBasis{basis, later} {
			if err := repo.Record(ctx, []domain.CandidateRejectionRecord{record(trackID, "yt:aaa", b)}); err != nil {
				t.Fatalf("Record: %v", err)
			}
		}

		want := []domain.PriorRejection{{SourceKey: "yt:aaa", Basis: &later}}
		if got := active(t, repo, trackID, anHourAgo()); !reflect.DeepEqual(got, want) {
			t.Errorf("Active = %+v, want %+v", got, want)
		}
	})

	t.Run("a batch naming one source twice keeps the last", func(t *testing.T) {
		repo, trackID := newRepo(t), ids.NewTrackId()
		last := domain.RejectionBasis{ExpectedSeconds: 99, ReferenceMBID: "last"}

		err := repo.Record(ctx, []domain.CandidateRejectionRecord{record(trackID, "yt:aaa", basis), record(trackID, "yt:aaa", last)})
		if err != nil {
			t.Fatalf("Record: %v", err)
		}
		want := []domain.PriorRejection{{SourceKey: "yt:aaa", Basis: &last}}
		if got := active(t, repo, trackID, anHourAgo()); !reflect.DeepEqual(got, want) {
			t.Errorf("Active = %+v, want %+v", got, want)
		}
	})

	t.Run("active rejections come back in source key order", func(t *testing.T) {
		repo, trackID := newRepo(t), ids.NewTrackId()

		err := repo.Record(ctx, []domain.CandidateRejectionRecord{record(trackID, "yt:ccc", basis), record(trackID, "yt:aaa", basis), record(trackID, "yt:bbb", basis)})
		if err != nil {
			t.Fatalf("Record: %v", err)
		}
		var keys []string
		for _, p := range active(t, repo, trackID, anHourAgo()) {
			keys = append(keys, p.SourceKey)
		}
		if want := []string{"yt:aaa", "yt:bbb", "yt:ccc"}; !reflect.DeepEqual(keys, want) {
			t.Errorf("keys = %v, want %v", keys, want)
		}
	})

	t.Run("recording nothing is not an error", func(t *testing.T) {
		if err := newRepo(t).Record(ctx, nil); err != nil {
			t.Errorf("Record(nil) = %v, want nil", err)
		}
	})

	t.Run("a done context is refused", func(t *testing.T) {
		repo, trackID := newRepo(t), ids.NewTrackId()
		done, cancel := context.WithCancel(ctx)
		cancel()

		if err := repo.Record(done, []domain.CandidateRejectionRecord{record(trackID, "yt:aaa", basis)}); err == nil {
			t.Error("Record on a done context = nil, want an error")
		}
		if _, err := repo.Active(done, trackID, anHourAgo()); err == nil {
			t.Error("Active on a done context = nil, want an error")
		}
	})
}
