package memory_test

import (
	"altune/go-api/internal/library"
	"altune/go-api/internal/library/internal/adapters/contracttest"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/clock/clocktest"
	"testing"
	"time"
)

func TestOrphanedAudioFake_Contract(t *testing.T) {
	contracttest.OrphanedAudioQueueContract(t, func(*testing.T) (library.OrphanedAudioQueue, func(*library.Track)) {
		tracks := library.NewInMemoryTrackRepo()
		return library.NewInMemoryOrphanedAudioQueue(tracks, clock.Now), tracks.Seed
	})
}

func TestOrphanedAudioFake_RefusesANilClock(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("NewInMemoryOrphanedAudioQueue with a nil clock did not panic, want a refusal at construction")
		}
	}()
	library.NewInMemoryOrphanedAudioQueue(library.NewInMemoryTrackRepo(), nil)
}

func TestOrphanedAudioFake_StampsRecordsWithTheInjectedClock(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	manual := clocktest.NewManual(start)
	q := library.NewInMemoryOrphanedAudioQueue(library.NewInMemoryTrackRepo(), manual.Func())

	if err := q.RecordOrphanedAudio(t.Context(), library.OrphanedAudio{AudioRef: "audio/a.opus"}); err != nil {
		t.Fatal(err)
	}
	if got := q.Orphans["audio/a.opus"].RecordedAt; !got.Equal(start) {
		t.Errorf("RecordedAt = %v, want the injected clock's %v", got, start)
	}
}
