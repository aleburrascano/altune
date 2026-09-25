package handler

import (
	"altune/go-api/internal/shared"
	"log/slog"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
)

type overseerEvent struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	User      string    `json:"user,omitempty"`
	Subject   string    `json:"subject,omitempty"`
	CorrID    string    `json:"corr_id,omitempty"`
}

type overseerLogRecord struct {
	Time    time.Time         `json:"time"`
	Level   string            `json:"level"`
	Message string            `json:"msg"`
	Fields  map[string]string `json:"attrs,omitempty"`
}

func assertFrameCarriesEveryKey(t *testing.T, frame string, mirror reflect.Type) {
	t.Helper()
	fields := decodeFrame(t, frame)
	for _, key := range jsonKeys(mirror) {
		if _, present := fields[key]; !present {
			t.Errorf("frame %s lacks %q, which Overseer's %s decodes", frame, key, mirror.Name())
		}
	}
}

func TestContract_EventFrameCarriesEveryKeyOverseerDecodes(t *testing.T) {
	fx := newStreamFixture(t)
	srv := httptest.NewServer(streamRouter(fx.deps))
	t.Cleanup(srv.Close)
	conn := openStream(t, srv, "/events/stream")

	marker := "contract-" + uuid.NewString()
	fx.tap.Publish(withCorrelation("contract-corr"), shared.NewUserId(uuid.New()), marker, map[string]any{"track_id": "trk-1"})

	assertFrameCarriesEveryKey(t, conn.awaitData(t, marker), reflect.TypeOf(overseerEvent{}))
}

func TestContract_LogFrameCarriesEveryKeyOverseerDecodes(t *testing.T) {
	fx := newStreamFixture(t)
	srv := httptest.NewServer(streamRouter(fx.deps))
	t.Cleanup(srv.Close)
	conn := openStream(t, srv, "/logs/stream")

	marker := "contract-" + uuid.NewString()
	slog.InfoContext(withCorrelation("contract-corr"), marker, slog.String("stage", "resolve"))

	assertFrameCarriesEveryKey(t, conn.awaitData(t, marker), reflect.TypeOf(overseerLogRecord{}))
}
