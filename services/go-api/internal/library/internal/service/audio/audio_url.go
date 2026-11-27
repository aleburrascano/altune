package audio

import (
	"altune/go-api/internal/library/internal/core/ports"
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
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

type AudioLinkService struct {
	trackRepo ports.TrackBatchGetter
	signer    ports.AudioLinkSigner
	canSign   bool
	metrics   ports.AudioStoreMetrics
	now       clock.Func
}

var ErrClockRequired = errors.New("audio: the audio link service requires a clock")

func NewAudioURLService(trackRepo ports.TrackBatchGetter, signer ports.AudioLinkSigner, now clock.Func, metrics ports.AudioStoreMetrics) (*AudioLinkService, error) {
	if now == nil {
		return nil, ErrClockRequired
	}
	if metrics == nil {
		panic("audio: NewAudioURLService requires metrics")
	}
	if signer == nil {
		panic("audio: NewAudioURLService requires a signer, wire NoAudioLinks when the store cannot sign")
	}
	return &AudioLinkService{trackRepo: trackRepo, signer: signer, canSign: signer.SignsLinks(), metrics: metrics, now: now}, nil
}

func MustNewAudioURLService(trackRepo ports.TrackBatchGetter, signer ports.AudioLinkSigner, now clock.Func, metrics ports.AudioStoreMetrics) *AudioLinkService {
	s, err := NewAudioURLService(trackRepo, signer, now, metrics)
	if err != nil {
		panic(err)
	}
	return s
}

func (s *AudioLinkService) Resolve(ctx context.Context, userId ids.UserId, trackIds []domain.TrackId) ([]ResolvedAudioURL, error) {
	if !s.canSign {
		return nil, nil
	}

	dbStart := s.now()
	tracks, err := s.trackRepo.ListByIDs(ctx, userId, trackIds)
	dbDuration := s.now().Sub(dbStart)
	if err != nil {
		return nil, domain.WrapRepoError(ctx, "resolve audio urls", err)
	}
	byID := make(map[domain.TrackId]*domain.Track, len(tracks))
	for _, t := range tracks {
		byID[t.ID()] = t
	}

	expiresAt := s.now().Add(audioURLTTL)
	out := make([]ResolvedAudioURL, 0, len(trackIds))
	presignStart := s.now()
	for _, id := range trackIds {
		track, found := byID[id]
		if !found || !track.IsStreamable() {
			continue
		}

		url, err := s.signer.PresignGet(ctx, *track.AudioRef(), audioURLTTL)
		if err != nil {
			s.metrics.PresignFailed()
			slog.WarnContext(ctx, "audio_url.presign_failed", logging.KeyTrackID, id.String(), logging.KeyError, err)
			continue
		}
		out = append(out, ResolvedAudioURL{TrackID: id, URL: url, Version: track.AudioVersion(), ExpiresAt: expiresAt})
	}
	slog.InfoContext(ctx, "audio_url.resolved",
		"requested", len(trackIds),
		"resolved", len(out),
		"db_lookup_ms", dbDuration.Milliseconds(),
		"presign_ms", s.now().Sub(presignStart).Milliseconds(),
	)
	return out, nil
}
