package service

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/logging"
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// corrCapturingRepo records the correlation ID carried by the context the
// background job hands to the acquisition service. The job context is built in
// scheduler.go from s.baseCtx, so before the fix it carried no link to the
// originating request and this observed empty.
type corrCapturingRepo struct {
	*fakeTrackRepository
	mu     sync.Mutex
	corrID string
	seen   bool
}

func (r *corrCapturingRepo) GetByID(ctx context.Context, id domain.TrackId, userId shared.UserId) (*domain.Track, error) {
	r.mu.Lock()
	r.corrID = logging.CorrelationIDFromContext(ctx)
	r.seen = true
	r.mu.Unlock()
	return r.fakeTrackRepository.GetByID(ctx, id, userId)
}

// TestBackgroundScheduler_ThreadsCorrelationIDIntoJobContext reproduces the
// defect: a scheduled job's context must carry the request's correlation ID so
// every slog.*Context call made deep in the acquisition pipeline traces back to
// the originating request. Red before the job context was derived with the
// request's corr_id; green once Schedule threads r.Context() through.
func TestBackgroundScheduler_ThreadsCorrelationIDIntoJobContext(t *testing.T) {
	repo := &corrCapturingRepo{fakeTrackRepository: newFakeTrackRepository()}
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	var wg sync.WaitGroup
	sem := make(chan struct{}, 1)
	scheduler := NewBackgroundAcquisitionScheduler(svc, &wg, sem)

	const wantCorrID = "corr-xyz-345"
	ctx := logging.WithCorrelationID(context.Background(), wantCorrID)
	scheduler.Schedule(ctx, shared.NewUserId(uuid.New()), domain.NewTrackId(), "")

	wg.Wait()

	repo.mu.Lock()
	defer repo.mu.Unlock()
	if !repo.seen {
		t.Fatal("acquisition service was never invoked by the scheduled job")
	}
	if repo.corrID != wantCorrID {
		t.Fatalf("job context lost the request correlation id: got %q, want %q", repo.corrID, wantCorrID)
	}
}
