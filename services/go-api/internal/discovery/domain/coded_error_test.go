package domain

import (
	"errors"
	"net/http"
	"testing"
)

func TestCodedErrors_PinStatusCodeAndMessage(t *testing.T) {
	cases := []struct {
		name       string
		err        *CodedError
		wantStatus int
		wantCode   string
		wantMsg    string
	}{
		{"rate limited", ErrRateLimited, http.StatusTooManyRequests, "discovery.rate_limited", "too many requests, try again later"},
		{"favorites full, limit 0", NewFavoritesFullError(0), http.StatusConflict, "discovery.favorites_full", "favorites are full: at most 0 per account"},
		{"favorites full, real limit", NewFavoritesFullError(1000), http.StatusConflict, "discovery.favorites_full", "favorites are full: at most 1000 per account"},
		{"invalid favorite, empty", NewInvalidFavoriteError(""), http.StatusBadRequest, "discovery.invalid_favorite", ""},
		{"invalid favorite, message", NewInvalidFavoriteError("favorite needs a title"), http.StatusBadRequest, "discovery.invalid_favorite", "favorite needs a title"},
		{"invalid event, empty", NewInvalidEventError(""), http.StatusBadRequest, "discovery.invalid_event", ""},
		{"invalid event, message", NewInvalidEventError("event_id must be a non-nil UUID"), http.StatusBadRequest, "discovery.invalid_event", "event_id must be a non-nil UUID"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err.HTTPStatus() != tc.wantStatus {
				t.Errorf("status = %d, want %d", tc.err.HTTPStatus(), tc.wantStatus)
			}
			if tc.err.ErrorCode() != tc.wantCode {
				t.Errorf("code = %q, want %q", tc.err.ErrorCode(), tc.wantCode)
			}
			if tc.err.Error() != tc.wantMsg {
				t.Errorf("message = %q, want %q", tc.err.Error(), tc.wantMsg)
			}
		})
	}
}

func TestCodedError_LookAlikeIsNotMatched(t *testing.T) {
	var coded *CodedError
	if errors.As(errors.New("discovery.invalid_event"), &coded) {
		t.Fatal("a plain error was matched as a CodedError")
	}
}
