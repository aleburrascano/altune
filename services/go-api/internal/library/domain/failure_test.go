package domain_test

import (
	"altune/go-api/internal/library"
	"altune/go-api/internal/shared/downloadfailure"
	"testing"
)

func TestFailureAliasesForwardToShared(t *testing.T) {
	reason := library.JoinFailureReason(library.FailureNoConfidentMatch, "none")
	if reason != "no_confident_match"+downloadfailure.FailureDetailSeparator+"none" {
		t.Errorf("JoinFailureReason = %q", reason)
	}
	code, detail := library.SplitFailureReason(reason)
	if code != library.FailureNoConfidentMatch || detail != "none" {
		t.Errorf("SplitFailureReason = %q, %q", code, detail)
	}
	if !code.Known() {
		t.Error("Known = false")
	}
	if got := library.FailureMessage(&reason); got != "Couldn't find this track" {
		t.Errorf("FailureMessage = %q", got)
	}
}
