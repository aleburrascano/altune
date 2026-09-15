package liveactivity

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

// TestCollectStoreRenderLiveFeed is the core Done proof: events flow from the
// source into the bounded ring and render as a live feed, watched-app text
// escaped.
func TestCollectStoreRenderLiveFeed(t *testing.T) {
	src := newFakeSource(8)
	b := newBucket(src)

	if body := string(b.Render().Body); !strings.Contains(body, "no events yet") {
		t.Errorf("empty render = %q, want a no-events gap", body)
	}

	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	src.push(goapi.Event{Type: "track.played", Timestamp: when, User: "u1", Subject: "song a"})
	src.push(goapi.Event{Type: "search.performed", Subject: "<script>jazz"})
	collectStore(t, b)

	body := string(b.Render().Body)
	for _, want := range []string{"LIVE", "2 event(s)", "track.played", "user=u1", "song a", "In flight: 0"} {
		if !strings.Contains(body, want) {
			t.Errorf("render missing %q\n---\n%s", want, body)
		}
	}
	if strings.Contains(body, "<script>jazz") {
		t.Errorf("render leaked unescaped event text:\n%s", body)
	}
	if !strings.Contains(body, "&lt;script&gt;jazz") {
		t.Errorf("render did not HTML-escape event text:\n%s", body)
	}
	if b.Meta().ID != "liveactivity" {
		t.Errorf("Meta.ID = %q, want liveactivity", b.Meta().ID)
	}
}

// TestDegradesToStaleWhenSourceDown is the spine proof: drop the source and the
// bucket serves its last-known feed FLAGGED STALE, and the shell still responds
// (/health and the panel page), never crashing because one source is down.
func TestDegradesToStaleWhenSourceDown(t *testing.T) {
	src := newFakeSource(8)
	b := newBucket(src)
	src.push(goapi.Event{Type: "track.played", Subject: "last known song"})
	collectStore(t, b)

	if body := string(b.Render().Body); !strings.Contains(body, "LIVE") {
		t.Fatalf("pre-drop render = %q, want LIVE", body)
	}

	src.setStatus(goapi.StatusDown) // the source drops

	body := string(b.Render().Body)
	if !strings.Contains(body, "STALE") {
		t.Errorf("post-drop render = %q, want a STALE flag", body)
	}
	if !strings.Contains(body, "last known song") {
		t.Errorf("post-drop render dropped last-known state:\n%s", body)
	}

	// The shell must stay up serving the stale panel and health, with the bucket
	// wired in exactly as production wires it.
	handler := shell.NewHandler(fixedRegistry{[]core.Bucket{b}}).Router(testToken)

	shellRec := do(handler, authed(httptest.NewRequest(http.MethodGet, "/", nil)))
	if shellRec.Code != http.StatusOK {
		t.Fatalf("shell status = %d, want 200 with a stale bucket", shellRec.Code)
	}
	if sb := shellRec.Body.String(); !strings.Contains(sb, "STALE") || !strings.Contains(sb, "last known song") {
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

// TestStaysBoundedUnderLoad is the bounded-storage proof: feeding many times the
// ring capacity never grows retained storage past the cap (reusing core's ring).
func TestStaysBoundedUnderLoad(t *testing.T) {
	src := newFakeSource(4 * eventCapacity)
	b := newBucket(src)
	for i := 0; i < 4*eventCapacity; i++ {
		src.push(goapi.Event{Type: "flood", Subject: "e"})
	}
	// Drain in cycles the way the tick loop would.
	for b.events.Len() < eventCapacity {
		collectStore(t, b)
	}
	collectStore(t, b)

	if got := b.events.Len(); got != eventCapacity {
		t.Errorf("retained %d signals, want capped at %d", got, eventCapacity)
	}
	if !strings.Contains(string(b.Render().Body), "200 event(s)") {
		t.Errorf("render did not cap the feed at capacity")
	}
}

// TestConcurrentCollectAndRender is the concurrency attack: the collect/store
// cycle and Render run together (as the tick loop and HTTP handlers do) with no
// data race — run under -race.
func TestConcurrentCollectAndRender(t *testing.T) {
	src := newFakeSource(256)
	b := newBucket(src)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			select {
			case src.events <- goapi.Event{Type: "e", Subject: "x"}:
			default:
			}
			signals, _ := b.Collect(ctx)
			b.Store(signals)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			_ = b.Render()
		}
	}()
	wg.Wait()
}

// TestUnconfiguredDegradesNotCrashes proves the production constructor with no
// go-api env yields a bucket that renders stale and bounded, never panicking.
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
