package logging

import (
	"errors"
	"strings"
	"testing"
)

func TestSearchTextAttr_HidesTextButCorrelates(t *testing.T) {
	const text = "späte private query"
	a := SearchTextAttr(text)
	b := SearchTextAttr(text)
	other := SearchTextAttr("something else")

	if strings.Contains(a.Value.String(), "private") {
		t.Fatalf("attr leaks search text: %v", a.Value)
	}
	if a.Key != "search_text" || isSensitiveKey(a.Key) {
		t.Fatalf("attr key %q must be non-empty and survive ring-buffer redaction", a.Key)
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

func TestScrubSearchErr_RemovesRawAndEscapedForms(t *testing.T) {
	const text = "a&b c/d"
	err := errors.New(`get "https://x/search?q=a%26b+c%2Fd": raw a&b c/d path a&b%20c%2Fd: timeout`)
	got := ScrubSearchErr(err, text)
	for _, form := range []string{"a&b c/d", "a%26b+c%2Fd", "a&b%20c%2Fd"} {
		if strings.Contains(got, form) {
			t.Fatalf("scrubbed error still contains %q: %s", form, got)
		}
	}
	if !strings.Contains(got, "timeout") {
		t.Errorf("scrubbing must keep the cause: %s", got)
	}
	if ScrubSearchErr(nil, text) != "" || ScrubSearchText("keep", "  ") != "keep" {
		t.Error("nil error and blank text must be no-ops")
	}
}
