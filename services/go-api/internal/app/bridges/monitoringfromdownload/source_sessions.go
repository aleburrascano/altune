package monitoringfromdownload

import (
	"altune/go-api/internal/platform/monitoring"
	"context"
	"time"
)

type YouTubeSessionSource interface {
	YouTubeSignedOutSince(ctx context.Context) (time.Time, bool)
}

type sourceSessions struct {
	source YouTubeSessionSource
}

func NewSourceSessions(source YouTubeSessionSource) monitoring.SourceSessions {
	return sourceSessions{source: source}
}

func (s sourceSessions) YouTubeSignedOutSince(ctx context.Context) (time.Time, bool) {
	return s.source.YouTubeSignedOutSince(ctx)
}
