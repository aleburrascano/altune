package logging_test

import (
	"altune/go-api/internal/shared/logging"
	"errors"
	"strings"
	"testing"
)

func TestSearchTextAttr_HidesTextButCorrelates(t *testing.T) {
	const text = "späte private query"
	a := logging.SearchTextAttr(text)
	b := logging.SearchTextAttr(text)
	other := logging.SearchTextAttr("something else")

	if strings.Contains(a.Value.String(), "private") {
		t.Fatalf("attr leaks search text: %v", a.Value)
	}
	if a.Value.String() != b.Value.String() {
		t.Errorf("same text must yield the same fingerprint: %v vs %v", a.Value, b.Value)
	}
	if a.Value.String() == other.Value.String() {
		t.Errorf("different text must yield a different fingerprint")
	}
	if got := a.Value.Group()[0].Value.Int64(); got != 19 {
		t.Errorf("len = %d, want 19 runes", got)
	}
}

func TestSearchTextAttr_SurvivesRingRedaction(t *testing.T) {
	logger, ring := newCaptureLogger(t)

	logger.Info("search", logging.SearchTextAttr("späte private query"))

	snap := ring.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("snapshot len = %d, want 1", len(snap))
	}
	if snap[0].Attrs["search_text.len"] != "19" || len(snap[0].Attrs["search_text.fp"]) != 12 {
		t.Fatalf("search_text attrs = %+v, want len=19 and a 12-hex-digit fingerprint kept by redaction", snap[0].Attrs)
	}
}

func TestScrubSearchErr_RemovesRawAndEscapedForms(t *testing.T) {
	const text = "a&b c/d"
	err := errors.New(`get "https://x/search?q=a%26b+c%2Fd": raw a&b c/d path a&b%20c%2Fd: timeout`)
	got := logging.ScrubSearchErr(err, text)
	for _, form := range []string{"a&b c/d", "a%26b+c%2Fd", "a&b%20c%2Fd"} {
		if strings.Contains(got, form) {
			t.Fatalf("scrubbed error still contains %q: %s", form, got)
		}
	}
	if !strings.Contains(got, "timeout") {
		t.Errorf("scrubbing must keep the cause: %s", got)
	}
	if logging.ScrubSearchErr(nil, text) != "" || logging.ScrubSearchErr(errors.New("keep  "), "  ") != "keep  " {
		t.Error("nil error and blank text must be no-ops")
	}
}
