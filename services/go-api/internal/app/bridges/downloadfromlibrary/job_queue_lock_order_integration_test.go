package downloadfromlibrary_test

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/download/downloadtest"
	"altune/go-api/internal/library"
	"altune/go-api/internal/library/librarytest"
	"altune/go-api/internal/shared/dbtest"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestPostgresJobQueue_QueueWritesRacingACatalogWriteThatChangesAMirroredColumnNeverDeadlock(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	dbtest.ExclusiveAcquisitionQueue(t, pool)
	queue := newQueue(pool)
	repo := library.NewPostgresTrackRepository(pool, librarytest.DiscardDBCallMetrics())
	ctx := context.Background()

	ops := []struct {
		name string
		run  func(id ids.TrackId) error
	}{
		{"claim", func(ids.TrackId) error {
			_, err := queue.Claim(ctx, time.Minute)
			if errors.Is(err, downloadtest.ErrNoJobAvailable) {
				return nil
			}
			return err
		}},
		{"enqueue", func(id ids.TrackId) error { return queue.Enqueue(ctx, id, downloadtest.JobKindAcquire, 0) }},
		{"heartbeat", func(id ids.TrackId) error { return queue.Heartbeat(ctx, id, 1, time.Minute) }},
		{"release", func(id ids.TrackId) error { return queue.Release(ctx, id, 1, time.Second) }},
		{"settle", func(id ids.TrackId) error { return queue.Settle(ctx, id, 1) }},
	}
	for _, op := range ops {
		t.Run(op.name, func(t *testing.T) {
			for i := 0; i < 30; i++ {
				track := insertPendingTrack(t, pool, time.Now().Add(-time.Minute))
				if op.name != "claim" && op.name != "enqueue" {
					if _, err := queue.Claim(ctx, time.Minute); err != nil {
						t.Fatalf("round %d: Claim = %v", i, err)
					}
				}
				loaded, err := repo.GetByID(ctx, track.ID, track.UserId)
				if err != nil {
					t.Fatalf("GetByID = %v", err)
				}
				changed := time.Now().Add(-time.Duration(i+1) * time.Second)
				librarytest.AmendTrack(loaded, func(s *librarytest.TrackSnapshot) { s.AcquisitionStartedAt = &changed })

				var wg sync.WaitGroup
				var opErr, updateErr error
				start := make(chan struct{})
				wg.Add(2)
				go func() { defer wg.Done(); <-start; opErr = op.run(track.ID) }()
				go func() { defer wg.Done(); <-start; updateErr = repo.Update(ctx, loaded, loaded.Version()) }()
				close(start)
				wg.Wait()

				if opErr != nil && !errors.Is(opErr, download.ErrLeaseLost) {
					t.Fatalf("round %d: %s = %v", i, op.name, opErr)
				}
				if updateErr != nil && !errors.Is(updateErr, library.ErrTrackVersionConflict) {
					t.Fatalf("round %d: Update = %v, want nil or a version conflict, never a deadlock", i, updateErr)
				}
				_, _ = pool.Exec(ctx, `DELETE FROM tracks WHERE id = $1`, track.ID.UUID())
			}
		})
	}
}
