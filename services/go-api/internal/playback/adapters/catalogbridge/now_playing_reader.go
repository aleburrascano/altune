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
)

var nowPlayingLookupTimeout = 3 * time.Second

// errEnrichmentUnavailable is returned when the fast-fail breaker is open, so
// resume degrades instantly instead of stalling for the full per-call timeout.
var errEnrichmentUnavailable = errors.New("now-playing enrichment temporarily unavailable")

var _ ports.NowPlayingReader = (*NowPlayingReader)(nil)

type trackReader interface {
	GetByID(ctx context.Context, id catalogDomain.TrackId, userId shared.UserId) (*catalogDomain.Track, error)
}

type NowPlayingReader struct {
	tracks  trackReader
	breaker *enrichmentBreaker
	metrics ports.PlaybackMetrics
}

func NewNowPlayingReader(tracks trackReader, opts ...func(*NowPlayingReader)) *NowPlayingReader {
	r := &NowPlayingReader{tracks: tracks, breaker: newEnrichmentBreaker(), metrics: ports.NoopPlaybackMetrics()}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// WithNowPlayingMetrics injects the degradation-counter sink. Left as a
// functional option so the adapter stays constructible without a metrics
// backend (defaulting to a no-op).
func WithNowPlayingMetrics(m ports.PlaybackMetrics) func(*NowPlayingReader) {
	return func(r *NowPlayingReader) {
		if m != nil {
			r.metrics = m
		}
	}
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
		// A malformed persisted track ID is a data defect, not "nothing
		// playing": surface it distinctly instead of folding it silently into
		// the empty-queue path. Log the raw value's shape (length), never the
		// value itself, alongside the owning user so it is diagnosable.
		slog.WarnContext(ctx, "now_playing.malformed_track_id",
			"user_id", userId.String(), "raw_len", len(trackId), "error", err)
		return trackAbsent()
	}

	if !r.breaker.allow() {
		return nil, errEnrichmentUnavailable
	}

	callCtx, cancel := context.WithTimeout(ctx, nowPlayingLookupTimeout)
	defer cancel()

	track, err := r.tracks.GetByID(callCtx, id, userId)
	if err != nil {
		// A caller-side cancellation is the client disconnecting, not a
		// catalog outage, so it must not trip the breaker against a healthy
		// catalog. Only failures owned by the dependency count.
		if ctx.Err() == nil {
			r.breaker.recordFailure()
			r.metrics.EnrichmentFailed()
			if errors.Is(err, context.DeadlineExceeded) {
				r.metrics.NowPlayingLookupTimedOut()
			}
		}
		return nil, fmt.Errorf("lookup now-playing track: %w", err)
	}
	r.breaker.recordSuccess()
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
