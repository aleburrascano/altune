package service

import (
	"altune/go-api/internal/shared"
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// A ready track whose audio still exists is the one input where the two
// service entry points diverge observably: Execute reconciles and skips it,
// ExecuteReplace searches for a new source. So it pins which one each
// scheduler method dispatches to.
func TestBackgroundScheduler_DispatchesToTheMatchingServiceEntryPoint(t *testing.T) {
	cases := []struct {
		name       string
		wantSearch bool
	}{
		{
			name:       "Schedule runs Execute",
			wantSearch: false,
		},
		{
			name:       "ScheduleReplace runs ExecuteReplace",
			wantSearch: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			userId := shared.NewUserId(uuid.New())
			repo := newFakeTrackRepository()
			track := readyTrackWithSource(t, repo, userId, "u/a/b/c.mp3", "https://youtube.com/watch?v=old")
			store := newFakeAudioStore()
			store.stored["u/a/b/c.mp3"] = true
			searcher := &fakeAudioSearcher{}
			svc := NewAcquireTrackAudioService(repo, fakeRegistry(searcher), store)

			var wg sync.WaitGroup
			scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, make(chan struct{}, 1))
			if tc.wantSearch {
				scheduler.ScheduleReplace(context.Background(), userId, track.ID)
			} else {
				scheduler.Schedule(context.Background(), userId, track.ID, "")
			}
			wg.Wait()

			if searcher.searchCalled != tc.wantSearch {
				t.Errorf("search called = %v, want %v", searcher.searchCalled, tc.wantSearch)
			}
		})
	}
}
