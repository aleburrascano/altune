package handler

import (
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestContract_EventFrameMatchesFixture(t *testing.T) {
	fx := newStreamFixture(t)
	srv := httptest.NewServer(streamRouter(fx.deps))
	t.Cleanup(srv.Close)
	conn := openStream(t, srv, "/events/stream")

	marker := "contract-" + uuid.NewString()
	fx.tap.Publish(withCorrelation("contract-corr"), shared.NewUserId(uuid.New()), events.EventType(marker), map[string]any{"track_id": "trk-1"})

	assertMatchesFixture(t, "observe_events_frame.json", []byte(conn.awaitData(t, marker)))
}

func TestContract_LogFrameMatchesFixture(t *testing.T) {
	fx := newStreamFixture(t)
	srv := httptest.NewServer(streamRouter(fx.deps))
	t.Cleanup(srv.Close)
	conn := openStream(t, srv, "/logs/stream")

	marker := "contract-" + uuid.NewString()
	slog.InfoContext(withCorrelation("contract-corr"), marker, slog.String("stage", "resolve"))

	assertMatchesFixture(t, "observe_logs_frame.json", []byte(conn.awaitData(t, marker)))
}
