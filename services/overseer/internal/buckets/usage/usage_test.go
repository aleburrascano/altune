package usage

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"altune/overseer/internal/shell"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testToken = "owner-token-owner-token-owner-tok" // >= 32 chars for the shell guard

// fakeSource is a controllable stand-in for the SSE consumer: a test pushes
// events onto it and flips its status, so the collect/render/degrade paths are
// exercised deterministically with no network and no wall-clock timing.
type fakeSource struct {
	events chan goapi.Event
	status atomic.Int32
}

func newFakeSource(buf int) *fakeSource {
	f := &fakeSource{events: make(chan goapi.Event, buf)}
	f.status.Store(int32(goapi.StatusUp))
	return f
}

func (f *fakeSource) Run(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
func (f *fakeSource) Events() <-chan goapi.Event    { return f.events }
func (f *fakeSource) Status() goapi.Status          { return goapi.Status(f.status.Load()) }
func (f *fakeSource) setStatus(s goapi.Status)      { f.status.Store(int32(s)) }
func (f *fakeSource) push(ev goapi.Event)           { f.events <- ev }

// collectStore runs one collect/store cycle, the pair the shell drives on a tick.
func collectStore(t *testing.T, b *Bucket) {
	t.Helper()
	signals, err := b.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	b.Store(signals)
}

// TestCollectStoreRenderRollups is the core Done proof: search and play events
// flow from the source into bounded rollups and render as top searches, per-kind
// play counts, and an activity timeline — watched-app query text escaped.
func TestCollectStoreRenderRollups(t *testing.T) {
	src := newFakeSource(16)
	b := newBucket(src)

	if body := string(b.Render().Body); !strings.Contains(body, "no searches yet") {
		t.Errorf("empty render = %q, want a no-searches gap", body)
	}

	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	src.push(goapi.Event{Type: "search_performed", Timestamp: when, Subject: "jazz"})
	src.push(goapi.Event{Type: "search_performed", Timestamp: when, Subject: "jazz"})
	src.push(goapi.Event{Type: "search_performed", Timestamp: when, Subject: "<script>funk"})
	src.push(goapi.Event{Type: "play", Timestamp: when, Subject: "song a"})
	src.push(goapi.Event{Type: "play", Timestamp: when, Subject: "song b"})
	src.push(goapi.Event{Type: "skip", Timestamp: when, Subject: "song c"})
	collectStore(t, b)

	body := string(b.Render().Body)
	for _, want := range []string{"LIVE", "Top searches", "jazz — 2", "Plays by kind", "play — 2", "skip — 1", "Activity timeline", "03:04 — 6"} {
		if !strings.Contains(body, want) {
			t.Errorf("render missing %q\n---\n%s", want, body)
		}
	}
	if strings.Contains(body, "<script>funk") {
		t.Errorf("render leaked unescaped query text:\n%s", body)
	}
	if !strings.Contains(body, "&lt;script&gt;funk") {
		t.Errorf("render did not HTML-escape query text:\n%s", body)
	}
	if b.Meta().ID != "usage" {
		t.Errorf("Meta.ID = %q, want usage", b.Meta().ID)
	}
}

// TestDegradesToStaleWhenSourceDown is the spine proof: drop the source and the
// bucket serves its last-known rollups FLAGGED STALE, and the shell still responds
// (/health and the panel page), never crashing because one source is down.
func TestDegradesToStaleWhenSourceDown(t *testing.T) {
	src := newFakeSource(8)
	b := newBucket(src)
	src.push(goapi.Event{Type: "search_performed", Subject: "last known query"})
	collectStore(t, b)

	if body := string(b.Render().Body); !strings.Contains(body, "LIVE") {
		t.Fatalf("pre-drop render = %q, want LIVE", body)
	}

	src.setStatus(goapi.StatusDown) // the source drops

	body := string(b.Render().Body)
	if !strings.Contains(body, "STALE") {
		t.Errorf("post-drop render = %q, want a STALE flag", body)
	}
	if !strings.Contains(body, "last known query") {
		t.Errorf("post-drop render dropped last-known rollups:\n%s", body)
	}

	// The shell must stay up serving the stale panel and health, with the bucket
	// wired in exactly as production wires it.
	handler := shell.NewHandler(fixedRegistry{[]core.Bucket{b}}).Router(testToken)

	shellRec := do(handler, authed(httptest.NewRequest(http.MethodGet, "/", nil)))
	if shellRec.Code != http.StatusOK {
		t.Fatalf("shell status = %d, want 200 with a stale bucket", shellRec.Code)
	}
	if sb := shellRec.Body.String(); !strings.Contains(sb, "STALE") || !strings.Contains(sb, "last known query") {
		t.Errorf("shell page missing stale last-known panel:\n%s", sb)
	}

	if healthRec := do(handler, httptest.NewRequest(http.MethodGet, "/health", nil)); healthRec.Code != http.StatusOK {
		t.Fatalf("/health status = %d, want 200 while source down", healthRec.Code)
	}
}

// TestCollectReportsSourceDownWithNoFreshEvents proves Collect follows the Bucket
// contract: an unreachable source with nothing fresh returns an error (so the
// shell keeps last-known state), while a healthy source returns no error.
func TestCollectReportsSourceDownWithNoFreshEvents(t *testing.T) {
	src := newFakeSource(1)
	b := newBucket(src)

	src.setStatus(goapi.StatusDown)
	if _, err := b.Collect(context.Background()); err == nil {
		t.Error("Collect returned nil while source down with no events, want an error")
	}

	src.setStatus(goapi.StatusUp)
	if _, err := b.Collect(context.Background()); err != nil {
		t.Errorf("Collect while up = %v, want nil", err)
	}
}

// TestConcurrentCollectAndRender is the concurrency attack: the collect/store
// cycle and Render run together (as the tick loop and HTTP handlers do) with no
// data race — run under -race.
func TestConcurrentCollectAndRender(t *testing.T) {
	src := newFakeSource(512)
	b := newBucket(src)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	kinds := []string{"search_performed", "play", "skip", "completed", "misc"}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			select {
			case src.events <- goapi.Event{Type: kinds[i%len(kinds)], Subject: "q", Timestamp: time.Unix(int64(i), 0)}:
			default:
			}
			signals, _ := b.Collect(ctx)
			b.Store(signals)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			_ = b.Render()
		}
	}()
	wg.Wait()
}

// TestUnconfiguredDegradesNotCrashes proves the production constructor with no
// go-api env yields a bucket that renders stale and bounded, never panicking, and
// opens its own null source rather than sharing another bucket's.
func TestUnconfiguredDegradesNotCrashes(t *testing.T) {
	t.Setenv("OVERSEER_GOAPI_URL", "")
	t.Setenv("OVERSEER_GOAPI_TOKEN", "")
	b := New()
	if _, ok := b.src.(*nullSource); !ok {
		t.Fatalf("unconfigured source = %T, want *nullSource", b.src)
	}
	if body := string(b.Render().Body); !strings.Contains(body, "STALE") {
		t.Errorf("unconfigured render = %q, want STALE", body)
	}
}

// TestFutureTimestampDoesNotFreezeTimeline is the epic-close hardening proof: a
// single far-future event (clock skew / an NTP jump / a poisoned event) must not
// ratchet the activity window into the future and freeze it. Because record folds
// any older event into the current window, one future timestamp would otherwise
// pin curStart 1000h ahead and swallow every real event until wall-clock caught up.
// toSignal clamps a future timestamp to now, so the window stays anchored to the
// observer's clock and later real-time events still count.
func TestFutureTimestampDoesNotFreezeTimeline(t *testing.T) {
	src := newFakeSource(8)
	b := newBucket(src)

	src.push(goapi.Event{Type: "play", Timestamp: time.Now().Add(1000 * time.Hour)})
	collectStore(t, b)

	// The current activity window must be anchored near now, not ratcheted 1000h
	// into the future by the out-of-spec timestamp.
	if cur := b.roll.line.curStart; cur.After(time.Now().Add(2 * time.Minute)) {
		t.Errorf("timeline window ratcheted to %v (far future); future timestamp not clamped to now", cur)
	}

	// A subsequent real-time event still lands in a present window rather than being
	// swallowed behind a frozen future window.
	src.push(goapi.Event{Type: "play"})
	collectStore(t, b)
	total := 0
	for _, w := range b.roll.snapshot().timeline {
		total += w.Count
	}
	if total != 2 {
		t.Errorf("timeline counted %d events, want 2", total)
	}
}

type fixedRegistry struct{ buckets []core.Bucket }

func (f fixedRegistry) Buckets() []core.Bucket { return f.buckets }

func authed(r *http.Request) *http.Request {
	r.Header.Set("Authorization", "Bearer "+testToken)
	return r
}

func do(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}
