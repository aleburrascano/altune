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
		t.Errorf("Kind = %s, want not_found", err.Code().Kind())
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

func TestNewError_WithTheZeroCodeIsAnInternalError(t *testing.T) {
	err := domain.NewError(domain.Code{}, "")

	if err.Code() != domain.CodeInternal || err.Error() != "Internal server error" {
		t.Errorf("zero code gave %q %q, want internal so an unset code never claims a client fault", err.Code(), err.Error())
	}
}

func TestCodedError_MatchesAnyErrorWithTheSameCode(t *testing.T) {
	sentinel := domain.NewError(domain.CodeAcquisitionCooldownActive, "cooldown active, try again later")

	if !errors.Is(sentinel.WithRetryAfter(time.Minute), sentinel) {
		t.Error("a retryable copy is not the sentinel it came from")
	}
	if !errors.Is(fmt.Errorf("retry track 9: %w", domain.NewError(domain.CodeAcquisitionCooldownActive, "other words")), sentinel) {
		t.Error("an error with the same code and other words is not the sentinel")
	}
	if errors.Is(domain.NewError(domain.CodeAcquisitionPaused, ""), sentinel) {
		t.Error("an error with another code matched the sentinel")
	}
	if errors.Is(errors.New("cooldown active, try again later"), sentinel) {
		t.Error("a plain error with the same text matched the sentinel")
	}
	if errors.Is(sentinel, (*domain.CodedError)(nil)) {
		t.Error("an error matched a nil target")
	}
}

func TestWithRetryAfter_NeverKeepsANegativeWait(t *testing.T) {
	err := domain.NewError(domain.CodeDiscoveryRateLimited, "").WithRetryAfter(-time.Second)

	if err.RetryAfter() != 0 {
		t.Errorf("RetryAfter = %s, want 0", err.RetryAfter())
	}
}

func TestKind_NamesItselfForLogsAndMessages(t *testing.T) {
	for kind, want := range map[domain.Kind]string{
		domain.KindInternal: "internal", domain.KindInvalid: "invalid", domain.KindUnauthenticated: "unauthenticated",
		domain.KindForbidden: "forbidden", domain.KindNotFound: "not_found", domain.KindConflict: "conflict",
		domain.KindTooLarge: "too_large", domain.KindRateLimited: "rate_limited", domain.KindUpstreamFailed: "upstream_failed",
		domain.KindUnavailable: "unavailable", domain.KindTimeout: "timeout", domain.Kind(99): "kind(99)",
	} {
		if got := kind.String(); got != want {
			t.Errorf("Kind(%d).String() = %q, want %q", int(kind), got, want)
		}
	}
}
