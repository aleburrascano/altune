package catalog_test

import (
	"altune/go-api/internal/playback/internal/adapters/catalog"
	"altune/go-api/internal/playback/internal/ports"
	"altune/go-api/internal/playback/internal/service"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
)

func testUser() ids.UserId {
	return ids.NewUserId(uuid.New())
}

func captureLogs(t *testing.T) *logging.Capture {
	t.Helper()
	return logging.CaptureJSON(t, slog.LevelDebug)
}

func newReader(tracks ports.TrackSource, metrics ports.EnrichmentMetrics, now func() time.Time, lookupTimeout time.Duration) *service.NowPlayingReader {
	return service.MustNewNowPlayingReader(catalog.MustNewGuardedSource(tracks, metrics, now, lookupTimeout))
}
