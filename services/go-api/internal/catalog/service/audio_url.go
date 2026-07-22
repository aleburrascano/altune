package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
)

// audioURLTTL bounds how long a minted URL streams. Short enough that a leaked
// URL grants one object for a listening session, not indefinitely; long enough
// to outlast a queue the user leaves paused for a while.
const audioURLTTL = time.Hour

// ResolvedAudioURL is one track's short-lived, directly-streamable audio URL.
type ResolvedAudioURL struct {
	TrackID   domain.TrackId
	URL       string
	ExpiresAt time.Time
}

// trackBatchReader is the narrow read this service actually calls, out of
// ports.TrackRepository's full surface.
type trackBatchReader interface {
	ListByIDs(ctx context.Context, userId shared.UserId, ids []domain.TrackId) ([]*domain.Track, error)
}

// AudioURLService mints presigned, directly-streamable URLs for a user's ready
// tracks so the native player streams from storage instead of proxying every
// byte through the API. Tracks the caller can't stream (not found, not owned,
// not ready) are omitted; the client falls back to the proxy endpoint for those.
// When the audio store cannot sign (filesystem / local dev), nothing is returned
// and every track falls back to the proxy.
type AudioURLService struct {
	trackRepo trackBatchReader
	signer    ports.AudioURLSigner // nil when the store can't presign
	ttl       time.Duration
}

func NewAudioURLService(trackRepo trackBatchReader, store ports.AudioStore) *AudioURLService {
	signer, _ := store.(ports.AudioURLSigner)
	return &AudioURLService{trackRepo: trackRepo, signer: signer, ttl: audioURLTTL}
}

// Resolve returns a presigned URL per streamable track. Non-streamable or unknown
// tracks are silently skipped (the client proxies them). A presign failure on one
// track skips only that track, never the batch.
func (s *AudioURLService) Resolve(ctx context.Context, userId shared.UserId, trackIds []domain.TrackId) ([]ResolvedAudioURL, error) {
	if s.signer == nil {
		return nil, nil
	}

	// One batch read for the whole request (up to the handler's 200-id cap) —
	// per-id GetByID here cost two queries per track on the hottest path after
	// search (every queue build).
	dbStart := time.Now()
	tracks, err := s.trackRepo.ListByIDs(ctx, userId, trackIds)
	dbDuration := time.Since(dbStart)
	if err != nil {
		return nil, fmt.Errorf("resolve audio url: %w", err)
	}
	byID := make(map[domain.TrackId]*domain.Track, len(tracks))
	for _, t := range tracks {
		byID[t.ID] = t
	}

	expiresAt := time.Now().Add(s.ttl)
	out := make([]ResolvedAudioURL, 0, len(trackIds))
	presignStart := time.Now()
	for _, id := range trackIds {
		track := byID[id]
		if track == nil || !track.IsStreamable() {
			continue
		}

		url, err := s.signer.PresignGet(ctx, *track.AudioRef, s.ttl)
		if err != nil {
			slog.WarnContext(ctx, "audio_url.presign_failed", "track_id", id.String(), "error", err)
			continue
		}
		out = append(out, ResolvedAudioURL{TrackID: id, URL: url, ExpiresAt: expiresAt})
	}
	slog.InfoContext(ctx, "audio_url.resolved",
		"requested", len(trackIds),
		"resolved", len(out),
		"db_lookup_ms", dbDuration.Milliseconds(),
		"presign_ms", time.Since(presignStart).Milliseconds(),
	)
	return out, nil
}
