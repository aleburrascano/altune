package fingerprintstatus

import (
	"altune/go-api/internal/acquisition"
	"context"
	"errors"
	"sync/atomic"
)

type Tracker struct {
	identifier acquisition.AudioIdentifier
	succeeded  atomic.Bool
}

func NewTracker(identifier acquisition.AudioIdentifier) *Tracker {
	return &Tracker{identifier: identifier}
}

func (t *Tracker) Identify(ctx context.Context, filePath string, fullLengthForPreview float64) (acquisition.RecordingMatch, error) {
	match, err := t.identifier.Identify(ctx, filePath, fullLengthForPreview)
	switch {
	case err == nil:
		t.succeeded.Store(true)
	case !errors.Is(err, acquisition.ErrIdentifyThrottled):
		t.succeeded.Store(false)
	}
	return match, err
}

func (t *Tracker) Verified() bool { return t.succeeded.Load() }
