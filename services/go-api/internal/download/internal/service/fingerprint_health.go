package service

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"context"
	"errors"
	"sync/atomic"
)

type FingerprintHealth struct {
	identifier ports.AudioIdentifier
	succeeded  atomic.Bool
}

var _ ports.AudioIdentifier = (*FingerprintHealth)(nil)

func NewFingerprintHealth(identifier ports.AudioIdentifier) *FingerprintHealth {
	return &FingerprintHealth{identifier: identifier}
}

func (h *FingerprintHealth) Identify(ctx context.Context, filePath string, fullLengthForPreview float64) (ports.RecordingMatch, error) {
	match, err := h.identifier.Identify(ctx, filePath, fullLengthForPreview)
	switch {
	case err == nil:
		h.succeeded.Store(true)
	case !errors.Is(err, domain.ErrIdentifyThrottled):
		h.succeeded.Store(false)
	}
	return match, err
}

func (h *FingerprintHealth) Verified() bool { return h.succeeded.Load() }
