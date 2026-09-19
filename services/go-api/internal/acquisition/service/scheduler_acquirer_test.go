package service

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// stubAcquirer is the seam's payoff: a scheduler job can be driven without a
// repository, a store, or a provider registry behind it.
type stubAcquirer struct {
	mu  sync.Mutex
	ran []string
}

func (s *stubAcquirer) Execute(context.Context, shared.UserId, domain.TrackId) error {
	s.record("Execute")
	return nil
}

func (s *stubAcquirer) ExecuteReplace(context.Context, shared.UserId, domain.TrackId) error {
	s.record("ExecuteReplace")
	return nil
}

func (s *stubAcquirer) record(entryPoint string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ran = append(s.ran, entryPoint)
}

func (s *stubAcquirer) entryPointsRun() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.ran...)
}

func TestBackgroundScheduler_RunsTheStubbedAcquirerEntryPoint(t *testing.T) {
	cases := map[string]struct {
		schedule func(*BackgroundAcquisitionScheduler, shared.UserId, domain.TrackId) error
		want     string
	}{
		"Schedule runs Execute": {
			schedule: func(s *BackgroundAcquisitionScheduler, userId shared.UserId, trackId domain.TrackId) error {
				return s.Schedule(context.Background(), userId, trackId, "")
			},
			want: "Execute",
		},
		"ScheduleReplace runs ExecuteReplace": {
			schedule: func(s *BackgroundAcquisitionScheduler, userId shared.UserId, trackId domain.TrackId) error {
				return s.ScheduleReplace(context.Background(), userId, trackId)
			},
			want: "ExecuteReplace",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			acq := &stubAcquirer{}
			var wg sync.WaitGroup
			scheduler := NewBackgroundAcquisitionScheduler(acq, &wg, make(chan struct{}, 1))

			if err := tc.schedule(scheduler, shared.NewUserId(uuid.New()), domain.NewTrackId()); err != nil {
				t.Fatalf("schedule: %v", err)
			}
			wg.Wait()

			got := acq.entryPointsRun()
			if len(got) != 1 || got[0] != tc.want {
				t.Errorf("entry points run = %v, want [%s]", got, tc.want)
			}
		})
	}
}
