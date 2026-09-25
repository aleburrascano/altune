// Package security is the Overseer's Security bucket: continuous, live proof that
// go-api still rejects the attacks we've been hardening against. Unlike the
// passive buckets it is ACTIVE — it fires a fixed suite of SAFE self-tests at
// go-api's own surface (unauthenticated /v1 reads must be rejected, operator
// /admin routes stay operator-only, a burst is shed, a malformed read is
// rejected not crashed) and renders the pass/fail verdict.
//
// The load-bearing decision is the fence: the prober has its OWN raw HTTP client
// (it must send unauthenticated and malformed requests, so it never reuses the
// operator goapi client), and every request is validated against an own-infra
// allowlist BEFORE a socket is opened — an off-allowlist target is refused, not
// sent (see prober.go). The suite issues GETs only, so no probe can mutate
// go-api state by construction. A recover-guarded, ctx-bound scheduler drives it
// on its own low-frequency ticker; when go-api is unreachable the bucket serves
// its last-known verdict flagged STALE rather than going dark.
//
// The bucket owns all its own files and self-registers with one blank import in
// the composition root (the additive-buckets invariant).
package security

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"context"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	// historyCapacity bounds the retained self-test summaries. The ring caps
	// memory by construction no matter how long the service runs.
	historyCapacity = 120
	// defaultInterval is the self-test cadence; tunable via
	// OVERSEER_SECURITY_INTERVAL. Hourly matches the brief: low volume, active.
	defaultInterval = time.Hour

	bucketID            = "security"
	seriesFindingsOpen  = "findings_open"
	seriesProbeFailures = "probe_failures"
)

// Bucket fires the fenced self-test suite on its own schedule and renders the
// pass/fail panel, degrading to a stale last-known verdict when go-api is
// unreachable.
type Bucket struct {
	scheduler *scheduler
	history   *core.RingStore
	series    core.Series

	// mu guards the last-known suite result and its stale flag, which the
	// scheduler goroutine writes and the HTTP render reads.
	mu     sync.RWMutex
	last   *suiteResult
	stale  bool
	reason string

	start sync.Once
}

// New builds the Security bucket from the environment. When go-api is not
// configured it falls back to a null prober: the bucket still registers and its
// panel renders stale rather than crashing the service at startup.
func New() *Bucket { return newBucket(clientFromEnv(), defaultSuite(), intervalFromEnv()) }

// newBucket is the injectable constructor tests use to supply a controllable
// prober, check set and interval; production goes through New. The bucket wires
// itself as the scheduler's sink so suite results flow into its state.
func newBucket(client prober, checks []check, interval time.Duration) *Bucket {
	b := &Bucket{history: core.NewRingStore(historyCapacity), series: discardSeries{}}
	b.scheduler = newScheduler(client, checks, interval, b.record)
	return b
}

func (b *Bucket) Meta() core.Meta {
	return core.Meta{ID: bucketID, Title: "Security"}
}

func (b *Bucket) UseSeries(s core.Series) {
	b.series = s
}

func (b *Bucket) KeySeries() string {
	return seriesFindingsOpen
}

func (b *Bucket) Rings() map[string]*core.RingStore {
	return map[string]*core.RingStore{"history": b.history}
}

// Start launches the self-test scheduler once, bound to the app-lifetime ctx the
// shell hands it — cancelled only at shutdown, so the scheduler survives the
// per-tick collect deadline (#1812) that killed it after one run when it was
// launched from Collect. The sync.Once makes a second Start a no-op, so the
// bucket owns exactly one scheduler goroutine however the shell drives it. The
// scheduler owns all storage (it writes results through record on its own
// cadence), so nothing here waits on it.
func (b *Bucket) Start(ctx context.Context) {
	b.start.Do(func() { go b.scheduler.run(ctx) })
}

// Collect is a no-op: the scheduler runs on the app-lifetime Start hook and
// writes suite results straight into the bounded history, so the shell's per-tick
// pass has nothing to gather here.
func (b *Bucket) Collect(context.Context) ([]core.Signal, error) {
	return nil, nil
}

// Store is a no-op: the scheduler records suite results directly into the
// bounded history, the same way the reliability poller owns its own store.
func (b *Bucket) Store([]core.Signal) {}

// Snapshot builds the security envelope from the last-known verdict and the
// bounded history. When go-api is currently unreachable the panel is source_down
// (last known verdict preserved); with no run yet it is stale; otherwise live.
// UpdatedAt is the last run's time.
func (b *Bucket) Snapshot() core.Snapshot {
	b.mu.RLock()
	last, stale, reason := b.last, b.stale, b.reason
	b.mu.RUnlock()

	data := suiteData(last)
	data.History = b.history.Snapshot()
	updated := time.Time{}
	if last != nil {
		updated = last.at
	}
	severity, headline := securityHealth(last)
	return core.Snapshot{
		ID:        b.Meta().ID,
		Title:     b.Meta().Title,
		State:     core.StaleState(stale, last != nil),
		Reason:    reason,
		Severity:  severity,
		Headline:  headline,
		UpdatedAt: updated,
		Data:      core.MarshalData(data),
	}
}

// record is the scheduler's sink. A run that reached go-api replaces the
// last-known verdict and appends a bounded-history summary; a run where NO check
// reached go-api is a source outage, so it degrades to stale — keeping the
// last-known verdict rather than dropping the panel.
func (b *Bucket) record(res suiteResult) {
	b.recordSeries(res)
	if !res.reachedAny() {
		b.markStale()
		return
	}
	b.mu.Lock()
	r := res
	b.last, b.stale, b.reason = &r, false, ""
	b.mu.Unlock()
	b.history.Add(summarySignal(res))
}

func (b *Bucket) recordSeries(res suiteResult) {
	b.series.Record(bucketID, seriesFindingsOpen, core.Point{At: res.at, Value: float64(res.failing())})
	b.series.Record(bucketID, seriesProbeFailures, core.Point{At: res.at, Value: float64(res.unreached())})
}

// markStale flags the last-known verdict stale while preserving it — serve
// last-known flagged stale rather than going dark (degrade, don't crash).
func (b *Bucket) markStale() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stale = true
	b.reason = goapi.ReasonDown
}

// clientFromEnv builds the fenced prober from OVERSEER_GOAPI_URL and the
// allowlist. Missing or invalid config yields a null prober so an unconfigured
// bucket degrades to stale instead of failing the whole service at startup.
// Config normally lives in the config package, which this leaf may not edit;
// reading the go-api knobs here keeps the change within the bucket, matching the
// reliability and domainquality buckets.
func clientFromEnv() prober {
	base := strings.TrimSpace(os.Getenv("OVERSEER_GOAPI_URL"))
	if base == "" {
		return nullProber{}
	}
	c, err := newFencedClient(base, allowlistFromEnv(base))
	if err != nil {
		slog.Warn("security: cannot build fenced client, degrading to source-down", "error", err)
		return nullProber{}
	}
	return c
}

// allowlistFromEnv reads the own-infra allowlist from OVERSEER_SECURITY_ALLOWLIST
// (comma-separated hosts). When unset it defaults to the configured go-api host
// and nothing else — the fence is the explicit, testable gate, not implicit
// trust of the base URL. An explicit list that omits the base host fails closed
// in newFencedClient, surfacing the misconfiguration.
func allowlistFromEnv(base string) []string {
	raw := strings.TrimSpace(os.Getenv("OVERSEER_SECURITY_ALLOWLIST"))
	if raw == "" {
		return []string{base}
	}
	return strings.Split(raw, ",")
}

// intervalFromEnv reads the tunable self-test cadence, defaulting to hourly. A
// non-positive or unparseable value is refused in favour of the default rather
// than silently disabling the self-test.
func intervalFromEnv() time.Duration {
	raw := strings.TrimSpace(os.Getenv("OVERSEER_SECURITY_INTERVAL"))
	if raw == "" {
		return defaultInterval
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		slog.Warn("security: invalid OVERSEER_SECURITY_INTERVAL, using default",
			"value", raw, "default", defaultInterval)
		return defaultInterval
	}
	return d
}

type discardSeries struct{}

func (discardSeries) Record(string, string, core.Point) {}

func (discardSeries) Query(string, string, time.Time, time.Time) ([]core.Point, error) {
	return nil, nil
}

func init() { core.Register(New()) }
