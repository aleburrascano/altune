package handler

import (
	"altune/go-api/internal/playback/internal/domain"
	apierror "altune/go-api/internal/shared/apierror"
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

const statusClientClosedRequest = 499

const queueStateRetryAfter = time.Second

var errQueueRateLimited = apierror.NewError(apierror.CodePlaybackRateLimited, "too many queue state requests, try again later")

type mappedError struct {
	coded *apierror.CodedError
	cause error
}

func (e *mappedError) Error() string   { return e.cause.Error() }
func (e *mappedError) Unwrap() []error { return []error{e.coded, e.cause} }

func writeQueueError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, context.Canceled) && r.Context().Err() != nil {
		slog.InfoContext(r.Context(), "playback.queue_state.client_cancelled",
			logging.KeyMethod, r.Method, logging.KeyPath, r.URL.Path, logging.KeyError, err)
		w.WriteHeader(statusClientClosedRequest)
		return
	}
	httpserver.HandleServiceError(w, r, mapQueueError(err))
}

func mapQueueError(err error) error {
	var invalid *domain.ValidationError
	switch {
	case errors.As(err, &invalid):
		return &mappedError{coded: apierror.NewError(validationCode(invalid.Kind), invalid.Error()), cause: err}
	case errors.Is(err, domain.ErrStaleQueueWrite):
		return &mappedError{coded: apierror.NewError(apierror.CodePlaybackStaleQueueWrite, domain.ErrStaleQueueWrite.Error()), cause: err}
	case errors.Is(err, domain.ErrQueuePositionMismatch):
		return &mappedError{coded: apierror.NewError(apierror.CodePlaybackQueuePositionMismatch, domain.ErrQueuePositionMismatch.Error()), cause: err}
	case errors.Is(err, domain.ErrQueueStateUnavailable):
		return &mappedError{
			coded: apierror.NewError(apierror.CodePlaybackUnavailable, domain.ErrQueueStateUnavailable.Error()).WithRetryAfter(queueStateRetryAfter),
			cause: err,
		}
	}
	return err
}

func validationCode(kind domain.ValidationKind) apierror.Code {
	switch kind {
	case domain.KindPositionMsNegative:
		return apierror.CodePlaybackPositionMsNegative
	case domain.KindQueueTooLong:
		return apierror.CodePlaybackQueueTooLong
	case domain.KindStringTooLong:
		return apierror.CodePlaybackStringTooLong
	case domain.KindStringContainsNul:
		return apierror.CodePlaybackStringContainsNul
	case domain.KindCurrentIdxOutOfRange:
		return apierror.CodePlaybackCurrentIdxOutOfRange
	case domain.KindCurrentTrackIdMissing:
		return apierror.CodePlaybackCurrentTrackIDMissing
	case domain.KindUnknownRepeatMode:
		return apierror.CodePlaybackUnknownRepeatMode
	case domain.KindUnknownSourceKind:
		return apierror.CodePlaybackUnknownSourceKind
	case domain.KindUnrecognizedSourceId:
		return apierror.CodePlaybackUnrecognizedSourceID
	case domain.KindMalformedTrackId:
		return apierror.CodePlaybackMalformedTrackID
	case domain.KindNaturalOrderMismatch:
		return apierror.CodePlaybackNaturalOrderMismatch
	}
	return apierror.CodeInternal
}
