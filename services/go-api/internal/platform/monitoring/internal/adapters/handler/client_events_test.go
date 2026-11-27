package handler_test

import (
	"altune/go-api/internal/platform/monitoring/internal/adapters/handler"
	"altune/go-api/internal/platform/monitoring/internal/core/domain"
	"altune/go-api/internal/shared/apierror"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/requestuser"
	"altune/go-api/internal/shared/telemetry"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type stubRecorder struct {
	recorded []domain.ClientEvent
	err      error
}

func (s *stubRecorder) Record(_ context.Context, e domain.ClientEvent) error {
	if s.err != nil {
		return s.err
	}
	s.recorded = append(s.recorded, e)
	return nil
}

type stubSink struct{ emitted []telemetry.ClientEvent }

func (s *stubSink) Emit(_ context.Context, e telemetry.ClientEvent) { s.emitted = append(s.emitted, e) }

func clientEventsRouter(t *testing.T, rec *stubRecorder, sink *stubSink, limit handler.ClientEventLimit) http.Handler {
	t.Helper()
	return handler.NewClientEventHandler(rec, sink, limit, time.Now).Routes()
}

func postClientEvent(t *testing.T, h http.Handler, owner ids.UserId, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/events", strings.NewReader(body))
	req = req.WithContext(requestuser.ContextWithUserID(req.Context(), owner))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(telemetry.AppVersionHeader, "1.2.3")
	req.Header.Set(telemetry.PlatformHeader, "ios")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestClientEvents_ARecordedEventAnswers204AndIsEmittedWithTheBoundedCorrelationId(t *testing.T) {
	rec, sink := &stubRecorder{}, &stubSink{}
	owner := ids.NewUserId(uuid.New())
	h := clientEventsRouter(t, rec, sink, handler.DefaultClientEventLimit)

	resp := postClientEvent(t, h, owner, `{"type":"client_error","event_id":"6f1c2f6e-8f3a-4f0e-9d1c-0a3b6a8c9d10","correlation_id":"corr-1","client_occurred_at":"2026-11-27T10:00:00Z","payload":{"surface":"player"},"search_id":"ignored","query_norm":"ignored"}`)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("status = %d (%s), want 204", resp.Code, resp.Body)
	}
	if len(rec.recorded) != 1 {
		t.Fatalf("recorded = %d events, want 1", len(rec.recorded))
	}
	got := rec.recorded[0]
	if got.Type != domain.ClientEventClientError || got.UserId != owner || got.CorrelationId != "corr-1" || got.ClientOccurredAt.IsZero() {
		t.Errorf("recorded = %+v, want the typed event with the owner, correlation id and client time", got)
	}
	if len(sink.emitted) != 1 || sink.emitted[0].CorrelationID != "corr-1" || sink.emitted[0].AppVersion != "1.2.3" || sink.emitted[0].Platform != "ios" {
		t.Errorf("emitted = %+v, want one event carrying the correlation id and the app headers", sink.emitted)
	}
}

func TestClientEvents_AnOverlongCorrelationIdIsDroppedNotRejected(t *testing.T) {
	rec, sink := &stubRecorder{}, &stubSink{}
	h := clientEventsRouter(t, rec, sink, handler.DefaultClientEventLimit)

	resp := postClientEvent(t, h, ids.NewUserId(uuid.New()), `{"type":"screen_view","correlation_id":"`+strings.Repeat("x", 65)+`"}`)

	if resp.Code != http.StatusNoContent || len(rec.recorded) != 1 || rec.recorded[0].CorrelationId != "" {
		t.Errorf("status = %d, recorded = %+v, want 204 with the correlation id dropped", resp.Code, rec.recorded)
	}
}

func TestClientEvents_ACorrelationIdWithANULIsDroppedNotStored(t *testing.T) {
	rec := &stubRecorder{}
	h := clientEventsRouter(t, rec, &stubSink{}, handler.DefaultClientEventLimit)

	resp := postClientEvent(t, h, ids.NewUserId(uuid.New()), `{"type":"screen_view","correlation_id":"a\u0000b"}`)

	if resp.Code != http.StatusNoContent || len(rec.recorded) != 1 || rec.recorded[0].CorrelationId != "" {
		t.Errorf("status = %d, recorded = %+v, want 204 with the correlation id dropped", resp.Code, rec.recorded)
	}
}

func TestClientEvents_ATypeAnotherModuleOwnsIsRefusedBeforeAnythingIsStored(t *testing.T) {
	for _, typ := range []string{"results_shown", "play", "library_add", "search_performed", "nonsense", ""} {
		t.Run(typ, func(t *testing.T) {
			rec, sink := &stubRecorder{}, &stubSink{}
			h := clientEventsRouter(t, rec, sink, handler.DefaultClientEventLimit)

			resp := postClientEvent(t, h, ids.NewUserId(uuid.New()), `{"type":"`+typ+`"}`)

			if resp.Code != http.StatusBadRequest || !strings.Contains(resp.Body.String(), string(apierror.CodeDiscoveryInvalidEventType.String())) {
				t.Errorf("status = %d body = %s, want 400 with the invalid event type code", resp.Code, resp.Body)
			}
			if len(rec.recorded) != 0 || len(sink.emitted) != 0 {
				t.Errorf("recorded = %v emitted = %v, want nothing stored or emitted", rec.recorded, sink.emitted)
			}
		})
	}
}

func TestClientEvents_AMalformedBodyIs400(t *testing.T) {
	h := clientEventsRouter(t, &stubRecorder{}, &stubSink{}, handler.DefaultClientEventLimit)

	for name, body := range map[string]string{
		"not json":       `{`,
		"unknown field":  `{"type":"screen_view","surprise":1}`,
		"trailing data":  `{"type":"screen_view"} {}`,
		"wrong typed id": `{"type":"screen_view","event_id":5}`,
	} {
		t.Run(name, func(t *testing.T) {
			if resp := postClientEvent(t, h, ids.NewUserId(uuid.New()), body); resp.Code != http.StatusBadRequest {
				t.Errorf("status = %d (%s), want 400", resp.Code, resp.Body)
			}
		})
	}
}

func TestClientEvents_AnInvalidEventFromTheRecorderIs400AndIsNotEmitted(t *testing.T) {
	rec, sink := &stubRecorder{err: domain.NewInvalidClientEventError("event_id is required")}, &stubSink{}
	h := clientEventsRouter(t, rec, sink, handler.DefaultClientEventLimit)

	resp := postClientEvent(t, h, ids.NewUserId(uuid.New()), `{"type":"client_error"}`)

	if resp.Code != http.StatusBadRequest || len(sink.emitted) != 0 {
		t.Errorf("status = %d emitted = %v, want 400 and nothing emitted", resp.Code, sink.emitted)
	}
}

func TestClientEvents_AStoreFailureIsNotA2xxAndIsNotEmitted(t *testing.T) {
	rec, sink := &stubRecorder{err: errors.New("down")}, &stubSink{}
	h := clientEventsRouter(t, rec, sink, handler.DefaultClientEventLimit)

	resp := postClientEvent(t, h, ids.NewUserId(uuid.New()), `{"type":"screen_view"}`)

	if resp.Code < 500 || len(sink.emitted) != 0 {
		t.Errorf("status = %d emitted = %v, want a 5xx and nothing emitted", resp.Code, sink.emitted)
	}
}

func TestClientEvents_APastTheBudgetPostIs429ForThatUserOnly(t *testing.T) {
	h := clientEventsRouter(t, &stubRecorder{}, &stubSink{}, handler.ClientEventLimit{Max: 2, Window: time.Hour})
	busy, quiet := ids.NewUserId(uuid.New()), ids.NewUserId(uuid.New())

	for range 2 {
		if resp := postClientEvent(t, h, busy, `{"type":"screen_view"}`); resp.Code != http.StatusNoContent {
			t.Fatalf("within budget: status = %d, want 204", resp.Code)
		}
	}

	if resp := postClientEvent(t, h, busy, `{"type":"screen_view"}`); resp.Code != http.StatusTooManyRequests {
		t.Errorf("over budget: status = %d, want 429", resp.Code)
	}
	if resp := postClientEvent(t, h, quiet, `{"type":"screen_view"}`); resp.Code != http.StatusNoContent {
		t.Errorf("another user: status = %d, want 204", resp.Code)
	}
}

func TestClientEvents_ABodyOverTheCapIs413(t *testing.T) {
	h := clientEventsRouter(t, &stubRecorder{}, &stubSink{}, handler.DefaultClientEventLimit)

	resp := postClientEvent(t, h, ids.NewUserId(uuid.New()), `{"type":"screen_view","payload":{"k":"`+strings.Repeat("x", 33<<10)+`"}}`)

	if resp.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", resp.Code)
	}
}
