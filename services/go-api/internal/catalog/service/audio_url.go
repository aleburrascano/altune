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

const audioURLTTL = time.Hour

type ResolvedAudioURL struct {
	TrackID   domain.TrackId
	URL       string
	ExpiresAt time.Time
}

type trackBatchReader interface {
	ListByIDs(ctx context.Context, userId shared.UserId, ids []domain.TrackId) ([]*domain.Track, error)
}

type AudioURLService struct {
	trackRepo trackBatchReader
	signer    ports.AudioURLSigner
	ttl       time.Duration
}

func NewAudioURLService(trackRepo trackBatchReader, store ports.AudioStore) *AudioURLService {
	signer, _ := store.(ports.AudioURLSigner)
	return &AudioURLService{trackRepo: trackRepo, signer: signer, ttl: audioURLTTL}
}

func (s *AudioURLService) Resolve(ctx context.Context, userId shared.UserId, trackIds []domain.TrackId) ([]ResolvedAudioURL, error) {
	if s.signer == nil {
		return nil, nil
	}

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
