package domain_test

import (
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/shared/music"
	"testing"
)

func TestFailureAliasesForwardToShared(t *testing.T) {
	reason := domain.JoinFailureReason(domain.FailureNoConfidentMatch, "none")
	if reason != "no_confident_match"+music.FailureDetailSeparator+"none" {
		t.Errorf("JoinFailureReason = %q", reason)
	}
	code, detail := domain.SplitFailureReason(reason)
	if code != domain.FailureNoConfidentMatch || detail != "none" {
		t.Errorf("SplitFailureReason = %q, %q", code, detail)
	}
	if !code.Known() {
		t.Error("Known = false")
	}
	if got := domain.FailureMessage(&reason); got != "Couldn't find this track" {
		t.Errorf("FailureMessage = %q", got)
	}
}
