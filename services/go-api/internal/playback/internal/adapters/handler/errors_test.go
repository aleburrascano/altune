package handler_test

import (
	"altune/go-api/internal/playback/internal/domain"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidationErrors_EachKindAnswers400WithItsWireCode(t *testing.T) {
	kinds := []domain.ValidationKind{
		domain.KindPositionMsNegative, domain.KindQueueTooLong, domain.KindStringTooLong, domain.KindStringContainsNul,
		domain.KindCurrentIdxOutOfRange, domain.KindCurrentTrackIdMissing, domain.KindUnknownRepeatMode,
		domain.KindUnknownSourceKind, domain.KindUnrecognizedSourceId, domain.KindMalformedTrackId, domain.KindNaturalOrderMismatch,
	}
	for _, kind := range kinds {
		t.Run(string(kind), func(t *testing.T) {
			repo := &recordingRepo{positionErr: fmt.Errorf("wrapped: %w", &domain.ValidationError{Kind: kind})}
			rec := httptest.NewRecorder()

			newHandler(repo).Routes().ServeHTTP(rec, positionPut(`{"current_index":0,"current_track_id":"11111111-1111-1111-1111-111111111111","position_ms":1}`))

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body %s", rec.Code, rec.Body.String())
			}
			if want := `"code":"playback.` + string(kind) + `"`; !strings.Contains(rec.Body.String(), want) {
				t.Errorf("body %s lacks %s", rec.Body.String(), want)
			}
		})
	}
}

func TestStoreErrors_AnswerTheirStatusCodeAndRetryAfter(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		status     int
		code       string
		retryAfter bool
	}{
		{"stale write", domain.ErrStaleQueueWrite, http.StatusConflict, "playback.stale_queue_write", false},
		{"position mismatch", domain.ErrQueuePositionMismatch, http.StatusConflict, "playback.queue_position_mismatch", false},
		{"unavailable", fmt.Errorf("%w: %w", domain.ErrQueueStateUnavailable, context.DeadlineExceeded), http.StatusServiceUnavailable, "playback.unavailable", true},
		{"unmapped", fmt.Errorf("disk on fire"), http.StatusInternalServerError, "internal", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()

			newHandler(&recordingRepo{positionErr: tc.err}).Routes().ServeHTTP(rec, positionPut(`{"current_index":0,"current_track_id":"11111111-1111-1111-1111-111111111111","position_ms":1}`))

			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, tc.status, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `"code":"`+tc.code+`"`) {
				t.Errorf("body %s lacks code %s", rec.Body.String(), tc.code)
			}
			if got := rec.Header().Get("Retry-After") != ""; got != tc.retryAfter {
				t.Errorf("Retry-After present = %v, want %v", got, tc.retryAfter)
			}
		})
	}
}
