package fingerprintstatus

import (
	"altune/go-api/internal/download"
	"context"
	"errors"
	"sync/atomic"
)

type HealthTracker struct {
	identifier download.AudioIdentifier
	succeeded  atomic.Bool
}

func NewTracker(identifier download.AudioIdentifier) *HealthTracker {
	return &HealthTracker{identifier: identifier}
}

func (t *HealthTracker) Identify(ctx context.Context, filePath string, fullLengthForPreview float64) (download.RecordingMatch, error) {
	match, err := t.identifier.Identify(ctx, filePath, fullLengthForPreview)
	switch {
	case err == nil:
		t.succeeded.Store(true)
	case !errors.Is(err, download.ErrIdentifyThrottled):
		t.succeeded.Store(false)
	}
	return match, err
}

func (t *HealthTracker) Verified() bool { return t.succeeded.Load() }
