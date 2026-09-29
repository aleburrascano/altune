package catalogbridge

import (
	"altune/go-api/internal/playback/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	catalogDomain "altune/go-api/internal/catalog/domain"
	catalogPorts "altune/go-api/internal/catalog/ports"
)

var nowPlayingLookupTimeout = 3 * time.Second

var errEnrichmentUnavailable = errors.New("now-playing enrichment temporarily unavailable")

var _ ports.NowPlayingReader = (*NowPlayingReader)(nil)

type trackReader interface {
	GetByID(ctx context.Context, id catalogDomain.TrackId, userId shared.UserId) (*catalogDomain.Track, error)
}

type NowPlayingReader struct {
	tracks  trackReader
	breaker *enrichmentBreaker
	metrics ports.EnrichmentMetrics
}

func NewNowPlayingReader(tracks trackReader, opts ...func(*NowPlayingReader)) *NowPlayingReader {
	r := &NowPlayingReader{tracks: tracks, metrics: ports.NoopEnrichmentMetrics()}
	for _, opt := range opts {
		opt(r)
	}
	r.breaker = newEnrichmentBreaker(r.metrics)
	return r
}

func WithNowPlayingMetrics(m ports.EnrichmentMetrics) func(*NowPlayingReader) {
	return func(r *NowPlayingReader) {
		if m != nil {
			r.metrics = m
		}
	}
}

func isDependencyFailure(err error) bool {
	return errors.Is(err, catalogPorts.ErrDBTransient) || errors.Is(err, context.DeadlineExceeded)
}

func trackAbsent() (*ports.NowPlayingTrack, error) {
	return nil, nil
}

func (r *NowPlayingReader) Lookup(
	ctx context.Context,
	userId shared.UserId,
	trackId string,
) (*ports.NowPlayingTrack, error) {
	id, err := catalogDomain.ParseTrackId(trackId)
	if err != nil {
		slog.WarnContext(ctx, "now_playing.malformed_track_id",
			"user_id", userId.String(), "raw_len", len(trackId), "error", err)
		return trackAbsent()
	}

	admitted, admission := r.breaker.allow()
	if !admitted {
		r.metrics.EnrichmentBreakerRejected()
		return nil, errEnrichmentUnavailable
	}
	defer r.breaker.release(admission)

	callCtx, cancel := context.WithTimeout(ctx, nowPlayingLookupTimeout)
	defer cancel()

	track, err := r.tracks.GetByID(callCtx, id, userId)
	if err != nil {
		if ctx.Err() == nil {
			if isDependencyFailure(err) {
				r.breaker.recordFailure()
			}
			r.metrics.EnrichmentFailed()
			if errors.Is(err, context.DeadlineExceeded) {
				r.metrics.NowPlayingLookupTimedOut()
			}
		}
		return nil, fmt.Errorf("lookup now-playing track: %w", err)
	}
	r.breaker.recordSuccess(admission)
	if track == nil {
		return trackAbsent()
	}

	return &ports.NowPlayingTrack{
		Id:                track.ID.String(),
		Title:             track.Title,
		Artist:            track.Artist,
		ArtworkURL:        track.ArtworkURL,
		DurationSeconds:   track.DurationSeconds,
		AcquisitionStatus: track.AcquisitionStatus.String(),
	}, nil
}
