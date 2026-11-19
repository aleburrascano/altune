package domain_test

import (
	"altune/go-api/internal/catalog"
	shared "altune/go-api/internal/shared/kernel"
	"testing"
)

func TestFailureAliasesForwardToShared(t *testing.T) {
	reason := catalog.JoinFailureReason(catalog.FailureNoConfidentMatch, "none")
	if reason != "no_confident_match"+shared.FailureDetailSeparator+"none" {
		t.Errorf("JoinFailureReason = %q", reason)
	}
	code, detail := catalog.SplitFailureReason(reason)
	if code != catalog.FailureNoConfidentMatch || detail != "none" {
		t.Errorf("SplitFailureReason = %q, %q", code, detail)
	}
	if !code.Known() {
		t.Error("Known = false")
	}
	if got := catalog.FailureMessage(&reason); got != "Couldn't find this track" {
		t.Errorf("FailureMessage = %q", got)
	}
}
