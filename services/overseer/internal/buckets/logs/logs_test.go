package logs

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeSource is a controllable stand-in for the log SSE consumer. Records queued
// on its buffered channel are what the bucket drains; status is fixed so a test
// can assert the live vs stale (source-down) render paths deterministically.
type fakeSource struct {
	ch     chan goapi.LogRecord
	status goapi.Status
}

func newFakeSource(status goapi.Status, buffer int) *fakeSource {
	return &fakeSource{ch: make(chan goapi.LogRecord, buffer), status: status}
}

func (f *fakeSource) Run(ctx context.Context) error   { <-ctx.Done(); return ctx.Err() }
func (f *fakeSource) Records() <-chan goapi.LogRecord { return f.ch }
func (f *fakeSource) Status() goapi.Status            { return f.status }

func drive(t *testing.T, b *Bucket) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signals, _ := b.Collect(ctx)
	b.Store(signals)
}

// TestBucketRegisters proves the additive-buckets wiring: init self-registers the
// logs bucket into the process registry under a stable ID.
func TestBucketRegisters(t *testing.T) {
	found := false
	for _, bk := range core.Default.Buckets() {
		if bk.Meta().ID == "logs" {
			found = true
		}
	}
	if !found {
		t.Fatal("logs bucket did not self-register into the default registry")
	}
}

// TestCollectStoreRenderRoundTrip is the functional Done proof: records queued on
// the source are drained by Collect, stored, and rendered as a live tail — level,
// message and field all present.
func TestCollectStoreRenderRoundTrip(t *testing.T) {
	src := newFakeSource(goapi.StatusUp, 8)
	src.ch <- goapi.LogRecord{Time: time.Now().UTC(), Level: "INFO", Message: "queue resumed", Fields: map[string]string{"queue": "q1"}}
	src.ch <- goapi.LogRecord{Time: time.Now().UTC(), Level: "ERROR", Message: "boom", Fields: map[string]string{"err": "x"}}
	b := newBucket(src, "")

	drive(t, b)

	body := string(b.Render().Body)
	for _, want := range []string{"LIVE", "queue resumed", "queue=q1", "ERROR", "boom", "err=x", "2 line(s)"} {
		if !strings.Contains(body, want) {
			t.Fatalf("render missing %q; body = %s", want, body)
		}
	}
}

// TestRingStaysCapped is the resource-exhaustion proof: however many records
// arrive, the bounded RingStore never retains more than its capacity.
func TestRingStaysCapped(t *testing.T) {
	src := newFakeSource(goapi.StatusUp, 0)
	b := newBucket(src, "")
	over := logCapacity + 50
	signals := make([]core.Signal, 0, over)
	for i := 0; i < over; i++ {
		signals = append(signals, toSignal(goapi.LogRecord{Level: "INFO", Message: "x"}))
	}
	b.Store(signals)
	if got := b.records.Len(); got != logCapacity {
		t.Fatalf("ring retained %d records, want cap %d", got, logCapacity)
	}
	if got := b.records.Cap(); got != logCapacity {
		t.Fatalf("ring cap = %d, want %d", got, logCapacity)
	}
}

// TestLevelFilter proves the tail filters by minimum level, mirroring go-api's
// ranking: at ≥ WARN, INFO and DEBUG lines drop and WARN/ERROR remain.
func TestLevelFilter(t *testing.T) {
	records := []core.Signal{
		{Kind: "DEBUG", Text: `{"msg":"d"}`},
		{Kind: "INFO", Text: `{"msg":"i"}`},
		{Kind: "WARN", Text: `{"msg":"w"}`},
		{Kind: "ERROR", Text: `{"msg":"e"}`},
	}
	got := filterByLevel(records, "WARN")
	if len(got) != 2 {
		t.Fatalf("≥WARN kept %d records, want 2: %+v", len(got), got)
	}
	for _, s := range got {
		if s.Kind == "INFO" || s.Kind == "DEBUG" {
			t.Fatalf("≥WARN leaked a %s line", s.Kind)
		}
	}
	// An empty minimum shows everything.
	if all := filterByLevel(records, ""); len(all) != 4 {
		t.Fatalf("empty min kept %d records, want all 4", len(all))
	}
}

// TestRenderFiltersByConfiguredLevel proves the configured minimum level is
// applied end to end through Render, not only in the standalone filter.
func TestRenderFiltersByConfiguredLevel(t *testing.T) {
	src := newFakeSource(goapi.StatusUp, 4)
	src.ch <- goapi.LogRecord{Level: "INFO", Message: "info-line"}
	src.ch <- goapi.LogRecord{Level: "ERROR", Message: "error-line"}
	b := newBucket(src, "ERROR")

	drive(t, b)

	body := string(b.Render().Body)
	if strings.Contains(body, "info-line") {
		t.Fatalf("≥ERROR tail leaked an INFO line; body = %s", body)
	}
	if !strings.Contains(body, "error-line") {
		t.Fatalf("≥ERROR tail dropped the ERROR line; body = %s", body)
	}
	if !strings.Contains(body, "Level ≥ ERROR") {
		t.Fatalf("render did not surface the active level; body = %s", body)
	}
}

// TestDegradeToStale is the outlives-the-app proof: with the source down and
// nothing fresh, Collect signals source-down and Render serves the last-known
// tail flagged STALE rather than crashing or going blank.
func TestDegradeToStale(t *testing.T) {
	src := newFakeSource(goapi.StatusDown, 0)
	b := newBucket(src, "")

	// A prior line is retained from when the source was healthy.
	b.Store([]core.Signal{toSignal(goapi.LogRecord{Level: "INFO", Message: "last known"})})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := b.Collect(ctx); !errors.Is(err, errSourceDown) {
		t.Fatalf("Collect with down source returned %v, want errSourceDown", err)
	}
	body := string(b.Render().Body)
	if !strings.Contains(body, "STALE") {
		t.Fatalf("stale render missing STALE flag; body = %s", body)
	}
	if !strings.Contains(body, "last known") {
		t.Fatalf("stale render dropped the last-known tail; body = %s", body)
	}
}

// TestRenderEscapesEveryField is the render-escaping spine proof: a hostile
// message and a hostile field key AND value are all HTML-escaped, so no injected
// markup survives into the trusted panel HTML.
func TestRenderEscapesEveryField(t *testing.T) {
	src := newFakeSource(goapi.StatusUp, 1)
	src.ch <- goapi.LogRecord{
		Level:   "INFO",
		Message: `<script>alert('msg')</script>`,
		Fields: map[string]string{
			`<k>`: `<v>"&`,
		},
	}
	b := newBucket(src, "")

	drive(t, b)

	body := string(b.Render().Body)
	if strings.Contains(body, "<script>") {
		t.Fatalf("hostile message not escaped; body = %s", body)
	}
	if strings.Contains(body, "<k>") || strings.Contains(body, "<v>") {
		t.Fatalf("hostile field key/value not escaped; body = %s", body)
	}
	for _, want := range []string{"&lt;script&gt;", "&lt;k&gt;", "&lt;v&gt;", "&amp;"} {
		if !strings.Contains(body, want) {
			t.Fatalf("render missing escaped form %q; body = %s", want, body)
		}
	}
}

// TestRenderEscapesHostileEverything is the epic-close cross-cutting attack: the
// nastiest input driven through the whole toSignal -> RingStore -> decodeRecord
// -> render round trip at once — a hostile LEVEL (unknown, so normalizeLevel
// passes it through to the escaper), a message trying to close the tail's list
// and open a script, and a field key AND value carrying quotes and angle
// brackets. No attacker-supplied tag may survive into the marked-safe panel HTML.
func TestRenderEscapesHostileEverything(t *testing.T) {
	src := newFakeSource(goapi.StatusUp, 1)
	src.ch <- goapi.LogRecord{
		Level:   `</span><script>evil()</script>`,
		Message: `</li></ul><script>alert(1)</script><li>`,
		Fields: map[string]string{
			`k"<img src=x onerror=alert(1)>`: `v'"><script>bad()</script>`,
		},
	}
	b := newBucket(src, "")

	drive(t, b)

	body := string(b.Render().Body)
	// Every '<'/'>' in dynamic content must be escaped, so no hostile open-tag
	// substring can survive. A raw "onerror=" or "alert(1)" left as plain text is
	// inert precisely because its enclosing '<'/'>' were escaped.
	for _, raw := range []string{"<script", "<img", "<span><script", "<svg"} {
		if strings.Contains(body, raw) {
			t.Fatalf("raw hostile tag %q survived into panel HTML:\n%s", raw, body)
		}
	}
	for _, esc := range []string{"&lt;script&gt;", "&lt;img", "&lt;/li&gt;&lt;/ul&gt;", "&#34;", "&#39;"} {
		if !strings.Contains(body, esc) {
			t.Fatalf("expected escaped form %q missing (was it dropped instead?); body = %s", esc, body)
		}
	}
}
