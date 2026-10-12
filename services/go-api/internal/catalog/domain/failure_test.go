package domain

import "testing"

func TestFailureAliasesForwardToShared(t *testing.T) {
	reason := JoinFailureReason(FailureNoConfidentMatch, "none")
	if reason != "no_confident_match"+FailureDetailSeparator+"none" {
		t.Errorf("JoinFailureReason = %q", reason)
	}
	code, detail := SplitFailureReason(reason)
	if code != FailureNoConfidentMatch || detail != "none" {
		t.Errorf("SplitFailureReason = %q, %q", code, detail)
	}
	if !code.Known() {
		t.Error("Known = false")
	}
	if got := FailureMessage(&reason); got != "Couldn't find this track" {
		t.Errorf("FailureMessage = %q", got)
	}
}
