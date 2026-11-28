package handler_test

import (
	"altune/go-api/internal/search/internal/adapters/handler"
	"altune/go-api/internal/search/internal/domain"
	apierror "altune/go-api/internal/shared/apierror"
	"errors"
	"testing"
)

func TestCodedErrors_PinCodeKindAndMessage(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantKind apierror.Kind
		wantCode string
		wantMsg  string
	}{
		{"rate limited", handler.ErrRateLimited, apierror.KindRateLimited, "discovery.rate_limited", "too many requests, try again later"},
		{"favorites full, limit 0", handler.MapError(domain.NewFavoritesFullError(0)), apierror.KindConflict, "discovery.favorites_full", "favorites are full: at most 0 per account"},
		{"favorites full, real limit", handler.MapError(domain.NewFavoritesFullError(1000)), apierror.KindConflict, "discovery.favorites_full", "favorites are full: at most 1000 per account"},
		{"invalid favorite, empty", handler.MapError(domain.NewInvalidFavoriteError("")), apierror.KindInvalid, "discovery.invalid_favorite", "Invalid favorite"},
		{"invalid favorite, message", handler.MapError(domain.NewInvalidFavoriteError("favorite needs a title")), apierror.KindInvalid, "discovery.invalid_favorite", "favorite needs a title"},
		{"invalid event, empty", handler.MapError(domain.NewInvalidEventError("")), apierror.KindInvalid, "discovery.invalid_event", "Invalid search event"},
		{"invalid event, message", handler.MapError(domain.NewInvalidEventError("event_id must be a non-nil UUID")), apierror.KindInvalid, "discovery.invalid_event", "event_id must be a non-nil UUID"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var coded *apierror.CodedError
			if !errors.As(tc.err, &coded) {
				t.Fatalf("%v is not coded", tc.err)
			}
			if coded.Code().Kind() != tc.wantKind {
				t.Errorf("kind = %s, want %s", coded.Code().Kind(), tc.wantKind)
			}
			if coded.Code().String() != tc.wantCode {
				t.Errorf("code = %q, want %q", coded.Code(), tc.wantCode)
			}
			if coded.Error() != tc.wantMsg {
				t.Errorf("message = %q, want %q", coded.Error(), tc.wantMsg)
			}
		})
	}
}

func TestCodedError_LookAlikeIsNotMatched(t *testing.T) {
	var coded *apierror.CodedError
	if errors.As(errors.New("discovery.invalid_event"), &coded) {
		t.Fatal("a plain error was matched as a CodedError")
	}
}
