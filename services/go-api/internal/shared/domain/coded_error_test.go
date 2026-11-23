package domain_test

import (
	"altune/go-api/internal/shared/domain"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestNewError_CarriesItsCodeAndDetail(t *testing.T) {
	err := domain.NewError(domain.CodeCatalogTrackNotFound, "track 42 not found")

	if err.Error() != "track 42 not found" {
		t.Errorf("Error() = %q, want the detail", err.Error())
	}
	if err.Code() != domain.CodeCatalogTrackNotFound {
		t.Errorf("Code() = %s, want catalog.track_not_found", err.Code())
	}
	if err.Code().Kind() != domain.KindNotFound {
		t.Errorf("Kind = %d, want not found", err.Code().Kind())
	}
	if err.RetryAfter() != 0 {
		t.Errorf("RetryAfter = %s, want none", err.RetryAfter())
	}
}

func TestNewError_WithoutADetailSaysTheCodesTitle(t *testing.T) {
	err := domain.NewError(domain.CodeRequestTooLarge, "")

	if err.Error() != "Request body too large" {
		t.Errorf("Error() = %q, want the code's title", err.Error())
	}
}

func TestCodedError_IsFoundThroughWrapping(t *testing.T) {
	sentinel := domain.NewError(domain.CodeDiscoveryFavoritesFull, "favorites are full")
	wrapped := fmt.Errorf("add favorite for user 7: %w", sentinel)

	var coded *domain.CodedError
	if !errors.As(wrapped, &coded) || coded.Code() != domain.CodeDiscoveryFavoritesFull {
		t.Fatalf("errors.As found %v, want the favorites_full error", coded)
	}
	if !errors.Is(wrapped, sentinel) {
		t.Error("errors.Is lost the sentinel through wrapping")
	}
}

func TestWithRetryAfter_ReturnsARetryableCopyAndLeavesTheOriginal(t *testing.T) {
	base := domain.NewError(domain.CodePlaybackUnavailable, "")

	retryable := base.WithRetryAfter(3 * time.Second)

	if retryable.RetryAfter() != 3*time.Second {
		t.Errorf("RetryAfter = %s, want 3s", retryable.RetryAfter())
	}
	if base.RetryAfter() != 0 {
		t.Errorf("original RetryAfter = %s, want it untouched", base.RetryAfter())
	}
	if retryable.Code() != base.Code() || retryable.Error() != base.Error() {
		t.Errorf("copy = %s %q, want the same code and detail as the original", retryable.Code(), retryable.Error())
	}
}

func TestCode_ZeroValueIsAnInternalError(t *testing.T) {
	var unset domain.Code

	if unset.Kind() != domain.KindInternal {
		t.Errorf("zero Code kind = %d, want internal so an unset code never claims a client fault", unset.Kind())
	}
}
