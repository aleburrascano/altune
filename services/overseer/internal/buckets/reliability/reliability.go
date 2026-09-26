// Package reliability is the Overseer's health bucket: at a glance, is the app
// up, and what dependency is degraded — from a view that survives the app going
// down. It does two structurally independent things:
//
//   - Mirror: it reads go-api's operator dependency health (/observe/health) via
//     the read-only goapi client and renders DB/Redis/Auth pills, keeping a
//     bounded history. When that observe read is unreachable it serves the
//     last-known mirrored health flagged STALE rather than going dark.
//   - Own poll: an independent reachability poller hits go-api's open /health on
//     its own ticker and records up/down entirely from its own probes. This is
//     the authoritative down-detector — it shares no state with the observe-read
//     path, so "the app is down" can never be conflated with "the observe API is
//     degraded".
//
// The bucket owns all its own files and self-registers with one blank import in
// the composition root (the additive-buckets invariant).
package reliability

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	// historyCapacity bounds the retained dependency-health samples. The ring
	// caps memory by construction no matter how long the service runs.
	historyCapacity = 120
	// pollCapacity bounds the retained reachability-poll outcomes, kept separate
	// from the mirror history so the two paths share no store either.
	pollCapacity = 120
	// defaultPollInterval is the own-poll cadence; tunable via
	// OVERSEER_RELIABILITY_POLL_INTERVAL. 30s matches the brief's default.
	defaultPollInterval = 30 * time.Second

	bucketID        = "reliability"
	seriesUp        = "up"
	seriesLatencyMS = "latency_ms"
)

// errUnconfigured is the transport error a null client reports when go-api is
// not configured: the mirror renders stale and the poll reports down, rather
// than the whole service failing at startup.
var errUnconfigured = errors.New("reliability: go-api not configured")

// healthReader is the seam onto the mirror's admin-read path: the single goapi
// method the bucket needs to mirror dependency health. Depending on this
// interface (not the concrete *goapi.Client) keeps the admin-read path a
// distinct field from the poll path and lets a test drive it deterministically.
type healthReader interface {
	AdminHealth(ctx context.Context) (goapi.OperatorHealth, error)
}

// Bucket mirrors go-api's dependency health into a bounded ring and renders it as
// pills, alongside an independent reachability poll signal that is the
// authoritative down-detector.
type Bucket struct {
	reader  healthReader
	poller  *reachPoller
	history *core.RingStore

	// mu guards the last-known mirror snapshot and its stale flag, which the
	// collect loop writes and the HTTP render reads.
	mu          sync.RWMutex
	lastHealth  *goapi.OperatorHealth
	adminStale  bool
	adminReason string

	start   sync.Once
	running sync.WaitGroup
}

// New builds the Reliability bucket from the environment. When go-api is not
// configured it falls back to a null client: the bucket still registers, its
// poll reports down and its mirror renders stale rather than crashing.
func New() *Bucket {
	reader, checker := clientFromEnv()
	return newBucket(reader, checker, pollIntervalFromEnv())
}

// newBucket is the injectable constructor tests use to supply a controllable
// admin reader and an independent reachability checker; production goes through
// New. The reader and checker are separate parameters on purpose — the poll
// path's independence from the admin-read path is structural, not incidental.
func newBucket(reader healthReader, checker reachChecker, interval time.Duration) *Bucket {
	return &Bucket{
		reader:  reader,
		poller:  newReachPoller(checker, interval),
		history: core.NewRingStore(historyCapacity),
	}
}

func (b *Bucket) Meta() core.Meta {
	return core.Meta{ID: bucketID, Title: "Reliability"}
}

func (b *Bucket) UseSeries(s core.Series) {
	b.poller.series = s
}

func (b *Bucket) KeySeries() string {
	return seriesLatencyMS
}

// Start launches the independent reachability poller once, bound to the
// app-lifetime ctx the shell hands it — cancelled only at shutdown, so the poller
// survives the per-tick collect deadline (#1812) that froze it after one run when
// it was launched from Collect. The sync.Once makes a second Start a no-op, so the
// bucket owns exactly one poll goroutine however the shell drives it, and that
// goroutine exits when ctx is cancelled at shutdown so nothing leaks.
func (b *Bucket) Start(ctx context.Context) {
	b.start.Do(func() { b.running.Go(func() { b.poller.run(ctx) }) })
}

func (b *Bucket) Wait() {
	b.running.Wait()
}

func (b *Bucket) Rings() map[string]*core.RingStore {
	return map[string]*core.RingStore{"history": b.history, "poll": b.poller.samples}
}

// Collect mirrors go-api's operator health; the independent reachability poller
// that feeds the authoritative down-signal runs on the app-lifetime Start hook,
// not here. On a successful read it records the fresh snapshot and returns a
// bounded-history signal; when the admin read is unreachable it flags the mirror
// stale and returns an error, so the shell keeps the last-known pills and Render
// marks them STALE — the own poll signal is untouched and stays live either way.
func (b *Bucket) Collect(ctx context.Context) ([]core.Signal, error) {
	health, err := b.reader.AdminHealth(ctx)
	if err != nil {
		b.markAdminStale(err)
		return nil, fmt.Errorf("reliability: admin health unreachable: %w", err)
	}
	b.recordFresh(health)
	return []core.Signal{healthSignal(health)}, nil
}

func (b *Bucket) Store(signals []core.Signal) {
	for _, s := range signals {
		b.history.Add(s)
	}
}

// Data is the reliability panel payload: the authoritative own-poll reachability,
// the last-known mirrored dependency health (nil until first mirrored), whether
// that mirror is currently stale, the bounded health history, and the bounded
// reachability-poll history. Poll is the own-poll outcome ring the panel folds
// into an uptime figure and a reachability strip — the authoritative up/down
// signal, independent of the admin-health mirror. Dependency status/error strings
// are watched-app data carried raw; React escapes them.
type Data struct {
	Reachability string                `json:"reachability"`
	Health       *goapi.OperatorHealth `json:"health"`
	AdminStale   bool                  `json:"adminStale"`
	History      []core.Signal         `json:"history"`
	Poll         []core.Signal         `json:"poll"`
}

// Snapshot builds the reliability envelope. The own poll is the authoritative
// down-detector: when it reports go-api unreachable the panel is source_down (last
// known health preserved). A working poll with a currently-unreachable admin read
// is stale; otherwise live. UpdatedAt is the last mirrored health's check time.
func (b *Bucket) Snapshot() core.Snapshot {
	b.mu.RLock()
	last := b.lastHealth
	stale := b.adminStale
	reason := b.adminReason
	b.mu.RUnlock()

	outcome := b.poller.currentOutcome()
	reach := outcome.status()
	degraded := outcome.reason()
	reason = pollReason(reason, degraded)
	updated := time.Time{}
	if last != nil {
		updated = last.Detail.CheckedAt
	}
	poll := b.poller.samples.Snapshot()
	severity, headline := reliabilityHealth(reach, degraded != "", last, poll)
	return core.Snapshot{
		ID:        b.Meta().ID,
		Title:     b.Meta().Title,
		State:     reliabilityState(reach, stale),
		Reason:    reason,
		Severity:  severity,
		Headline:  headline,
		UpdatedAt: updated,
		Data: core.MarshalData(Data{
			Reachability: outcome.String(),
			Health:       last,
			AdminStale:   stale,
			History:      b.history.Snapshot(),
			Poll:         poll,
		}),
	}
}

// reliabilityHealth grades the watched app, not the mirror's freshness: the own
// poll finding go-api unreachable, or go-api itself reporting a dependency down,
// is a live failure; a window that holds a failed probe but is currently up has
// flapped, which is worth a look. The headline is uptime over the poll's bounded
// window — the one number this bucket exists to answer.
//
// A currently-unreachable ADMIN read is deliberately not graded here: that is the
// mirror being stale, which State already carries, and grading it would make
// severity a second freshness flag.
func reliabilityHealth(reach goapi.Status, degraded bool, health *goapi.OperatorHealth, poll []core.Signal) (core.Severity, string) {
	uptime := uptimeText(poll)
	switch {
	case reach == goapi.StatusDown:
		return core.SeverityCritical, "go-api unreachable · " + uptime
	case health != nil && !health.Healthy():
		return core.SeverityCritical, "dependency down · " + uptime
	case degraded:
		return core.SeverityWarn, "go-api degraded · " + uptime
	case hasFailedProbe(poll):
		return core.SeverityWarn, "recently flapped · " + uptime
	default:
		return core.SeverityOK, uptime
	}
}

func pollReason(adminReason, degraded string) string {
	switch {
	case adminReason == goapi.ReasonAuth, adminReason == goapi.ReasonThrottled:
		return adminReason
	case degraded != "":
		return degraded
	default:
		return adminReason
	}
}

// uptimeText renders the share of the poller's own probes that saw go-api up over
// its bounded window — the authoritative uptime, independent of the admin mirror.
func uptimeText(poll []core.Signal) string {
	if len(poll) == 0 {
		return "no reachability probes yet"
	}
	return fmt.Sprintf("uptime %.1f%%", float64(upProbes(poll))/float64(len(poll))*100)
}

// hasFailedProbe reports whether the bounded poll window holds a probe that found
// go-api down.
func hasFailedProbe(poll []core.Signal) bool { return upProbes(poll) < len(poll) }

// upProbes counts the probes in the window that found go-api reachable.
func upProbes(poll []core.Signal) int {
	up := 0
	for _, s := range poll {
		if s.Text == goapi.StatusUp.String() {
			up++
		}
	}
	return up
}

// reliabilityState derives the panel state. The own poll is authoritative for
// source-down; a degraded-but-reachable admin mirror is stale; a poll still
// pending its first result is stale; otherwise live.
func reliabilityState(reach goapi.Status, adminStale bool) core.State {
	switch reach {
	case goapi.StatusDown:
		return core.StateSourceDown
	case goapi.StatusConnecting:
		return core.StateStale
	case goapi.StatusUp:
		if adminStale {
			return core.StateStale
		}
		return core.StateLive
	default:
		return core.StateStale
	}
}

// recordFresh stores the latest mirrored health and clears the stale flag. The
// snapshot is copied to the heap and only ever replaced (never mutated in
// place), so Render may read the pointer under the lock and use it after.
func (b *Bucket) recordFresh(h goapi.OperatorHealth) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastHealth = &h
	b.adminStale = false
	b.adminReason = ""
}

// markAdminStale flags the mirror stale while preserving the last-known health,
// which is exactly the degrade-don't-crash behaviour: serve last-known flagged
// stale rather than dropping the panel.
func (b *Bucket) markAdminStale(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.adminStale = true
	b.adminReason = goapi.Classify(err)
}

// healthSignal renders one operator-health snapshot into the shared signal shape
// for the bounded history. The dependency statuses are watched-app data stored
// raw; they are HTML-escaped at render time.
func healthSignal(h goapi.OperatorHealth) core.Signal {
	at := h.Detail.CheckedAt
	if at.IsZero() {
		at = time.Now().UTC()
	}
	verdict := "healthy"
	if !h.Healthy() {
		verdict = "degraded"
	}
	return core.Signal{
		At:   at,
		Kind: "health",
		Text: fmt.Sprintf("db=%s redis=%s auth=%s (%s)", h.DB, h.Redis, h.Auth, verdict),
	}
}

// clientFromEnv builds the read-only goapi client from OVERSEER_GOAPI_URL and the
// process-wide operator token source, used as both the admin reader and the
// reachability checker. Missing or invalid config yields a null client so an
// unconfigured bucket degrades to source-down instead of failing the whole
// service at startup. Config normally lives in the config package, which this
// leaf may not edit; reading the go-api URL here and taking the credential from
// goapi.SharedTokenSource keeps the change within the bucket while sharing ONE
// token source with every other bucket (so refresh single-flights across them).
func clientFromEnv() (healthReader, reachChecker) {
	base := strings.TrimSpace(os.Getenv("OVERSEER_GOAPI_URL"))
	if base == "" {
		n := nullClient{}
		return n, n
	}
	c, err := goapi.New(base, goapi.SharedTokenSource())
	if err != nil {
		// Degrade to source-down, but say why: without this a URL typo is
		// indistinguishable from go-api being genuinely down.
		slog.Warn("reliability: invalid OVERSEER_GOAPI_URL, degrading to source-down", "error", err)
		n := nullClient{}
		return n, n
	}
	return c, c
}

// pollIntervalFromEnv reads the tunable own-poll cadence, defaulting to 30s. A
// non-positive or unparseable value is refused in favour of the default rather
// than silently disabling the detector.
func pollIntervalFromEnv() time.Duration {
	raw := strings.TrimSpace(os.Getenv("OVERSEER_RELIABILITY_POLL_INTERVAL"))
	if raw == "" {
		return defaultPollInterval
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		slog.Warn("reliability: invalid OVERSEER_RELIABILITY_POLL_INTERVAL, using default",
			"value", raw, "default", defaultPollInterval)
		return defaultPollInterval
	}
	return d
}

// nullClient stands in when go-api is unconfigured: every read reports
// source-down, so an unconfigured bucket's poll reports down and its mirror
// renders stale rather than nil-panicking. It satisfies both seams.
type nullClient struct{}

func (nullClient) AdminHealth(context.Context) (goapi.OperatorHealth, error) {
	return goapi.OperatorHealth{}, &goapi.SourceDownError{Op: "GET /observe/health", Err: errUnconfigured}
}

func (nullClient) Health(context.Context) (goapi.Health, error) {
	return goapi.Health{}, &goapi.SourceDownError{Op: "GET /health", Err: errUnconfigured}
}

func init() { core.Register(New()) }
