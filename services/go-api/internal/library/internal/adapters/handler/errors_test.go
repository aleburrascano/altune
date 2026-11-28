package handler_test

import (
	"altune/go-api/internal/library/internal/adapters/handler"
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/shared/apierror"
	"errors"
	"fmt"
	"testing"
)

func codeFor(t *testing.T, err error) *apierror.CodedError {
	t.Helper()
	var coded *apierror.CodedError
	if !errors.As(handler.MapError(err), &coded) {
		t.Fatalf("%v was not mapped to an apierror code", err)
	}
	return coded
}

func TestSentinelErrorCodes(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{domain.ErrTrackNotFound, "catalog.track_not_found"},
		{domain.ErrPlaylistNotFound, "catalog.playlist_not_found"},
		{domain.ErrAudioNotAvailable, "catalog.audio_not_available"},
		{domain.ErrAudioOrphaned, "catalog.audio_orphaned"},
		{domain.ErrCatalogTemporarilyUnavailable, "catalog.temporarily_unavailable"},
		{domain.ErrAudioTemporarilyUnavailable, "catalog.audio_temporarily_unavailable"},
		{domain.ErrStorageStalled, "catalog.audio_temporarily_unavailable"},
		{domain.ErrPlaylistFull, "catalog.playlist_full"},
		{domain.ErrLibraryFull, "catalog.library_full"},
	}
	for _, c := range cases {
		coded := codeFor(t, c.err)
		if got := coded.Code().String(); got != c.want {
			t.Errorf("%v: code = %q, want %q", c.err, got, c.want)
		}
		if got := coded.Error(); got != c.err.Error() {
			t.Errorf("%v: detail = %q, want the sentinel text", c.err, got)
		}
	}
}

func TestCatalogDomainErrorCodes(t *testing.T) {
	if got := codeFor(t, domain.ErrTrackAlreadyInPlaylist).Code().String(); got != "catalog.track_already_in_playlist" {
		t.Errorf("ErrTrackAlreadyInPlaylist code: got %q", got)
	}
	invalid := domain.NewValidationError("x")
	coded := codeFor(t, invalid)
	if got := coded.Code().String(); got != "catalog.validation_error" {
		t.Errorf("ValidationError code: got %q", got)
	}
	if coded.Error() != "x" {
		t.Errorf("ValidationError detail: got %q, want %q", coded.Error(), "x")
	}
}

func TestMapError_ThrottleErrorsKeepTheirKind(t *testing.T) {
	if got := codeFor(t, domain.ErrBackfillCoolingDown).Code().Kind(); got != apierror.KindRateLimited {
		t.Errorf("cooling down kind = %v, want rate limited", got)
	}
	if got := codeFor(t, domain.ErrBackfillInProgress).Code().Kind(); got != apierror.KindConflict {
		t.Errorf("in progress kind = %v, want conflict", got)
	}
}

func TestMapError_KeepsTheCauseReachableAndPassesUnknownErrorsThrough(t *testing.T) {
	wrapped := fmt.Errorf("delete track: %w", domain.ErrTrackNotFound)
	if mapped := handler.MapError(wrapped); !errors.Is(mapped, domain.ErrTrackNotFound) {
		t.Errorf("mapped error lost its cause: %v", mapped)
	}
	plain := errors.New("boom")
	if handler.MapError(plain) != plain {
		t.Error("an unknown error was rewritten, want it passed through for the 500 path")
	}
	reorder := fmt.Errorf("reorder: %w", domain.ErrPlaylistChangedDuringReorder)
	if got := codeFor(t, reorder).Code().String(); got != "catalog.validation_error" {
		t.Errorf("ErrPlaylistChangedDuringReorder code = %q, want a validation error", got)
	}
}
