package catalogbridge

import (
	"context"
	"errors"
	"testing"
	"time"

	catalogDomain "altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"

	"github.com/google/uuid"
)

type blockingTrackReader struct {
	entered chan struct{}
}

func (b *blockingTrackReader) GetByID(ctx context.Context, _ catalogDomain.TrackId, _ shared.UserId) (*catalogDomain.Track, error) {
	if b.entered != nil {
		close(b.entered)
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func testUser() shared.UserId {
	return shared.NewUserId(uuid.New())
}

func TestLookup_DerivesDeadlineWhenDependencyBlocks(t *testing.T) {
	prev := nowPlayingLookupTimeout
	nowPlayingLookupTimeout = 50 * time.Millisecond
	defer func() { nowPlayingLookupTimeout = prev }()

	reader := NewNowPlayingReader(&blockingTrackReader{entered: make(chan struct{})})

	done := make(chan error, 1)
	go func() {
		_, err := reader.Lookup(context.Background(), testUser(), uuid.New().String())
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want context.DeadlineExceeded", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Lookup did not return; no per-call deadline was derived from the request context")
	}
}
