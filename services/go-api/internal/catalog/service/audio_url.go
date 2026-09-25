package service

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"context"
	"fmt"
	"log/slog"
	"time"
)

const audioURLTTL = ports.MaxPresignTTL

type ResolvedAudioURL struct {
	TrackID   domain.TrackId
	URL       string
	Version   string
	ExpiresAt time.Time
}

type AudioURLService struct {
	trackRepo ports.TrackBatchGetter
	signer    ports.AudioURLSigner
	ttl       time.Duration
	metrics   ports.AudioStoreMetrics
	now       func() time.Time
}

func NewAudioURLService(trackRepo ports.TrackBatchGetter, store ports.AudioStore, opts ...func(*AudioURLService)) *AudioURLService {
	signer, _ := store.(ports.AudioURLSigner)
	s := &AudioURLService{trackRepo: trackRepo, signer: signer, ttl: audioURLTTL, metrics: ports.NoopAudioStoreMetrics(), now: time.Now}
	return applyOptions(s, opts)
}

func WithAudioURLMetrics(m ports.AudioStoreMetrics) func(*AudioURLService) {
	return func(s *AudioURLService) {
		if m != nil {
			s.metrics = m
		}
	}
}

// WithAudioURLClock replaces the clock the advertised expiry is measured from.
// A nil clock is ignored so the wall clock always holds.
func WithAudioURLClock(now func() time.Time) func(*AudioURLService) {
	return func(s *AudioURLService) {
		if now != nil {
			s.now = now
		}
	}
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

	// Clamp here too so the advertised expiry never outlives the signature the
	// storage boundary actually mints.
	ttl := ports.ClampPresignTTL(s.ttl)
	expiresAt := s.now().Add(ttl)
	out := make([]ResolvedAudioURL, 0, len(trackIds))
	presignStart := time.Now()
	for _, id := range trackIds {
		track := byID[id]
		if track == nil || !track.IsStreamable() {
			continue
		}

		url, err := s.signer.PresignGet(ctx, *track.AudioRef, ttl)
		if err != nil {
			s.metrics.PresignFailed()
			slog.WarnContext(ctx, "audio_url.presign_failed", "track_id", id.String(), "error", err)
			continue
		}
		out = append(out, ResolvedAudioURL{TrackID: id, URL: url, Version: track.AudioVersion, ExpiresAt: expiresAt})
	}
	slog.InfoContext(ctx, "audio_url.resolved",
		"requested", len(trackIds),
		"resolved", len(out),
		"db_lookup_ms", dbDuration.Milliseconds(),
		"presign_ms", time.Since(presignStart).Milliseconds(),
	)
	return out, nil
}
