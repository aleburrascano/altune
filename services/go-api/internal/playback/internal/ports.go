package ports

import (
	"altune/go-api/internal/playback/internal/domain"
	"altune/go-api/internal/shared/ids"
	"context"
)

type QueueStateRepository interface {
	Upsert(ctx context.Context, state *domain.QueueState) error
	UpdatePosition(ctx context.Context, position *domain.QueuePosition) error
	GetForUser(ctx context.Context, userId ids.UserId) (*domain.QueueState, error)
	DeleteForUser(ctx context.Context, userId ids.UserId) error
}

type QueueStateMetrics interface {
	CorruptStoredState()
	QueueStateOpTimedOut()
	QueueStateOpFailedTransient()
	QueueStateStaleWrite()
}

type RateLimitMetrics interface {
	QueueStateRateLimited()
}

type NowPlayingTrack struct {
	Id                string
	Title             string
	Artist            string
	ArtworkURL        *string
	DurationSeconds   *float64
	AcquisitionStatus string
}

type TrackSource interface {
	NowPlayingTrack(ctx context.Context, userId ids.UserId, id ids.TrackId) (*NowPlayingTrack, error)
}

type NowPlayingReader interface {
	Lookup(ctx context.Context, userId ids.UserId, trackId string) (*NowPlayingTrack, error)
}

type EnrichmentMetrics interface {
	EnrichmentFailed()
	NowPlayingLookupTimedOut()
	EnrichmentBreakerRejected()
	EnrichmentBreakerOpened()
	EnrichmentBreakerClosed()
}

type OwnerScanPage struct {
	Orphaned []ids.UserId
	Next     ids.UserId
	Done     bool
}

type DeletedUserDataLister interface {
	ListOwnersWithoutIdentity(ctx context.Context, after ids.UserId, window int) (OwnerScanPage, error)
}

type DeletedUserDataCleaner interface {
	ReapErasedStates(ctx context.Context) (int64, error)
}

type ErasureSweepMetrics interface {
	SweepIdle()
	QueueStateErased(n int)
	OwnerScanTimedOut()
	RetryOwnerDropped()
}
