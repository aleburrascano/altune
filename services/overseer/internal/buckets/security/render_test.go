package security

import (
	"altune/overseer/internal/core"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestRenderEscapesReflectedText proves render HTML-escapes any reflected text.
// A probe error can carry a target host or a transport message; if it were
// rendered raw, markup in it would inject into the trusted panel. The escape is
// the guard, so a poisoned error string must come out inert.
func TestRenderEscapesReflectedText(t *testing.T) {
	poison := `<script>alert('xss')</script>`
	res := suiteResult{
		at: time.Unix(0, 0).UTC(),
		results: []checkResult{
			{name: "unauth-v1", desc: "unauthenticated /v1 read is rejected", err: errors.New(poison)},
		},
	}
	history := []core.Signal{{Text: poison}}

	body := string(renderBody(&res, false, history))
	if strings.Contains(body, poison) {
		t.Errorf("render leaked raw markup from reflected text:\n%s", body)
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Errorf("render did not HTML-escape reflected text:\n%s", body)
	}
}

// TestRenderNilIsEmptyState proves render never panics before any run: a nil
// verdict is the explicit empty state.
func TestRenderNilIsEmptyState(t *testing.T) {
	body := string(renderBody(nil, false, nil))
	if !strings.Contains(body, "no security self-test has run yet") {
		t.Errorf("nil verdict render = %q, want empty-state text", body)
	}
}

// TestRenderShowsStale proves a stale verdict is flagged in the panel so the
// operator sees the result is last-known, not fresh.
func TestRenderShowsStale(t *testing.T) {
	res := suiteResult{
		at:      time.Unix(0, 0).UTC(),
		results: []checkResult{{name: "unauth-v1", desc: "d", passed: true, status: 401}},
	}
	body := string(renderBody(&res, true, nil))
	if !strings.Contains(body, "STALE") {
		t.Errorf("stale verdict not flagged:\n%s", body)
	}
}

// TestRenderShowsPassAndFail proves the panel distinguishes an all-pass verdict
// from one with a failing check.
func TestRenderShowsPassAndFail(t *testing.T) {
	pass := suiteResult{at: time.Unix(0, 0).UTC(), results: []checkResult{{desc: "d", passed: true, status: 401}}}
	if body := string(renderBody(&pass, false, nil)); !strings.Contains(body, "PASS") {
		t.Errorf("all-pass verdict not marked PASS:\n%s", body)
	}
	fail := suiteResult{at: time.Unix(0, 0).UTC(), results: []checkResult{{desc: "d", passed: false, status: 200}}}
	if body := string(renderBody(&fail, false, nil)); !strings.Contains(body, "FAIL") {
		t.Errorf("verdict with a failing check not marked FAIL:\n%s", body)
	}
}
