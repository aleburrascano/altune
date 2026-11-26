package domain_test

import (
	"altune/go-api/internal/search/internal/domain"
	apierror "altune/go-api/internal/shared/apierror"
	"errors"
	"testing"
)

func TestCodedErrors_PinCodeKindAndMessage(t *testing.T) {
	cases := []struct {
		name     string
		err      *apierror.CodedError
		wantKind apierror.Kind
		wantCode string
		wantMsg  string
	}{
		{"rate limited", domain.ErrRateLimited, apierror.KindRateLimited, "discovery.rate_limited", "too many requests, try again later"},
		{"favorites full, limit 0", domain.NewFavoritesFullError(0), apierror.KindConflict, "discovery.favorites_full", "favorites are full: at most 0 per account"},
		{"favorites full, real limit", domain.NewFavoritesFullError(1000), apierror.KindConflict, "discovery.favorites_full", "favorites are full: at most 1000 per account"},
		{"invalid favorite, empty", domain.NewInvalidFavoriteError(""), apierror.KindInvalid, "discovery.invalid_favorite", "Invalid favorite"},
		{"invalid favorite, message", domain.NewInvalidFavoriteError("favorite needs a title"), apierror.KindInvalid, "discovery.invalid_favorite", "favorite needs a title"},
		{"invalid event, empty", domain.NewInvalidEventError(""), apierror.KindInvalid, "discovery.invalid_event", "Invalid search event"},
		{"invalid event, message", domain.NewInvalidEventError("event_id must be a non-nil UUID"), apierror.KindInvalid, "discovery.invalid_event", "event_id must be a non-nil UUID"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err.Code().Kind() != tc.wantKind {
				t.Errorf("kind = %s, want %s", tc.err.Code().Kind(), tc.wantKind)
			}
			if tc.err.Code().String() != tc.wantCode {
				t.Errorf("code = %q, want %q", tc.err.Code(), tc.wantCode)
			}
			if tc.err.Error() != tc.wantMsg {
				t.Errorf("message = %q, want %q", tc.err.Error(), tc.wantMsg)
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
