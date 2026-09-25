// Package cost is the Overseer's Cost bucket: at a glance, what is Altune
// spending. The brief renders two independent signals — provider API usage (from
// go-api's cost-enabler) and OCI infra spend (from OCI's usage-api) — each
// degrading on its own, like the domain-quality bucket. This bucket builds both
// halves: the OCI infra-spend half and the provider-usage half.
//
// The OCI spend is read through a read-only usage-api client authenticated by
// instance principal (internal/oci). The provider usage is read through the
// read-only go-api client (internal/goapi), operator-authenticated, off the
// operator /observe/metrics/live "providers" field. The bucket depends only on the
// two read seams (spendReader, usageReader), never the OCI SDK or a live go-api,
// so fakes drive both in tests with no external auth. Neither source carries an
// identifier that must not leak (spend carries no OCI identifier; provider counts
// carry only bounded provider labels), so nothing sensitive reaches the panel or
// logs; and an unreachable source degrades to its last-known value flagged STALE
// rather than going dark. The two sources degrade
// INDEPENDENTLY: one source down flags only its own half stale, never the other.
// They also refresh on DIFFERENT cadences: the metered OCI spend is polled by its
// own slow scheduler (hourly by default), launched from the app-lifetime Start hook
// (#1950) — NOT the 5s collect tick — and served with its age between refreshes;
// the cheap provider-usage counter stays on the tick.
// The bucket owns all its own files and self-registers with one blank import in
// the composition root (the additive-buckets invariant).
package cost

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"altune/overseer/internal/oci"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"strings"
	"sync"
	"time"
)

// errUnconfigured is the error the null OCI client reports when OCI reads are not
// enabled: the spend half renders stale rather than the whole service failing at
// startup. Off an OCI instance (dev, CI) there is no instance principal, so this
// is the normal state until the bucket runs on the real box.
var errUnconfigured = errors.New("cost: OCI usage-api not enabled")

// errUsageUnconfigured is the transport error the null go-api client reports when
// go-api is not configured: the provider-usage half renders stale rather than the
// whole service failing at startup.
var errUsageUnconfigured = errors.New("cost: go-api not configured")

// errBothDown is returned by Collect only when the provider-usage read fails AND
// the OCI spend half is currently unreachable, so the shell logs a genuine full
// outage while Render keeps serving each half's last-known value flagged stale. A
// single source down never returns an error — that half degrades independently and
// the other stays live. Spend is refreshed off the tick by the scheduler, so its
// reachability is read from the cached stale flag, not a fresh read here. It wraps
// the usage cause (the read that actually failed on this tick).
var errBothDown = errors.New("cost: oci spend and provider usage both unreachable")

const (
	// defaultSpendInterval is the cadence the OCI billing-spend half refreshes on
	// when OVERSEER_COST_SPEND_INTERVAL is unset. Hourly matches how slowly a
	// metered month-to-date figure moves; the 5s collect tick would bill hundreds
	// of redundant usage-api reads an hour.
	defaultSpendInterval = time.Hour
	// spendIntervalEnvKey is the tunable spend-refresh cadence. config.Load
	// validates it at startup; the bucket reads it here to drive its own scheduler,
	// matching the security bucket's self-contained env read.
	spendIntervalEnvKey = "OVERSEER_COST_SPEND_INTERVAL"

	bucketID                = "cost"
	seriesSpendDaily        = "spend_daily"
	seriesSpendMonthToDate  = "spend_month_to_date"
	providerCallsSeriesStem = "provider_calls:"
)

// spendReader is the seam onto the OCI usage-api read the bucket needs — the one
// method from oci.UsageClient. Depending on this interface (not oci.Client) lets a
// test drive spend deterministically, including the degrade path, with no OCI auth.
type spendReader interface {
	CurrentPeriodSpend(ctx context.Context) (oci.Spend, error)
}

// usageReader is the seam onto the go-api provider-usage read — the one method
// from the goapi client. Depending on this interface (not *goapi.Client) lets a
// test drive provider counts deterministically, including the independent-degrade
// path, with no live go-api.
type usageReader interface {
	AdminProviderUsage(ctx context.Context) (goapi.ProviderUsage, error)
}

// Bucket reads OCI's current-period infra spend and go-api's per-provider call
// counts and renders both, flagging each half STALE independently when its source
// is currently unreachable while preserving the last-known value.
type Bucket struct {
	spend      spendReader
	usage      usageReader
	spendSched *spendScheduler
	start      sync.Once
	series     core.Series

	// mu guards the last-known snapshots, their stale flags and their per-half
	// update times: the spend scheduler goroutine and the collect loop write them
	// while the HTTP render reads them.
	mu                sync.RWMutex
	lastSpend         *oci.Spend
	spendStale        bool
	spendUpdated      time.Time
	spendBaseline     oci.Spend
	haveSpendBaseline bool
	lastUsage         *goapi.ProviderUsage
	usageStale        bool
	usageUpdated      time.Time
	usageReason       string
}

// New builds the Cost bucket from the environment. When OCI reads are not enabled
// or go-api is not configured it falls back to null clients: the bucket still
// registers and each half renders stale rather than crashing.
func New() *Bucket {
	return newBucket(spendReaderFromEnv(), usageReaderFromEnv(), spendIntervalFromEnv())
}

// newBucket is the injectable constructor tests use to supply controllable readers
// and a spend cadence; production goes through New. The bucket wires refreshSpend
// as the scheduler's poll so slow spend reads flow into its state.
func newBucket(spend spendReader, usage usageReader, spendInterval time.Duration) *Bucket {
	b := &Bucket{spend: spend, usage: usage, series: discardSeries{}}
	b.spendSched = newSpendScheduler(spendInterval, b.refreshSpend)
	return b
}

func (b *Bucket) Meta() core.Meta {
	return core.Meta{ID: bucketID, Title: "Cost"}
}

func (b *Bucket) UseSeries(s core.Series) {
	b.series = s
}

func (b *Bucket) KeySeries() string {
	return seriesSpendMonthToDate
}

// Start launches the OCI billing-spend scheduler once, bound to the app-lifetime
// ctx the shell hands it — cancelled only at shutdown, so the poller survives the
// per-tick collect deadline (#1812) that froze the prior attempt after one refresh
// when it was launched from Collect's ctx. The sync.Once makes a second Start a
// no-op, so the bucket owns exactly one scheduler goroutine however the shell drives
// it. The scheduler owns spend refreshes on its own cadence, so nothing here waits.
func (b *Bucket) Start(ctx context.Context) {
	b.start.Do(func() { go b.spendSched.run(ctx) })
}

// Collect reads the cheap provider-usage half on the tick. Spend is NOT read here:
// it refreshes on its own slow cadence via the Start-launched scheduler, so the
// metered usage-api is never hit on the 5s tick. The usage half records its fresh
// snapshot on success or is flagged stale on failure while its last-known value is
// preserved — the two halves remain INDEPENDENT. Collect returns an error only when
// the usage read fails AND the spend half is currently unreachable, so the shell
// logs a genuine full outage but never suppresses a half-live panel.
func (b *Bucket) Collect(ctx context.Context) ([]core.Signal, error) {
	usage, usageErr := b.usage.AdminProviderUsage(ctx)
	if usageErr != nil {
		b.markUsageStale(usageErr)
	} else {
		b.recordUsage(usage)
		b.recordProviderSeries(usage)
	}

	if usageErr != nil && b.spendUnreachable() {
		return nil, fmt.Errorf("%w: providers=%w (spend half also unreachable)", errBothDown, usageErr)
	}
	return nil, nil
}

// refreshSpend reads OCI spend once and records the outcome. A successful read
// replaces the last-known spend and clears its stale flag; a failed read preserves
// the last-known value flagged stale (degrade, don't crash). The scheduler calls
// this on its own slow cadence — never the collect tick.
func (b *Bucket) refreshSpend(ctx context.Context) {
	spend, err := b.spend.CurrentPeriodSpend(ctx)
	if err != nil {
		b.markSpendStale()
		return
	}
	b.recordSpend(spend)
}

// spendUnreachable reports whether the spend half currently has no live value: its
// last scheduled read failed, or none has landed yet. It lets Collect tell a
// genuine full outage from an independent usage-only degrade, reading the cached
// flag under the lock rather than issuing a fresh tick-bound read.
func (b *Bucket) spendUnreachable() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.spendStale || b.lastSpend == nil
}

// Store satisfies the bucket contract. The cost panel renders only the current
// spend and usage figures and keeps no bounded trend, so there is nothing to
// persist here.
func (b *Bucket) Store([]core.Signal) {}

// Data is the cost panel payload: the OCI infra-spend half and the go-api
// provider-usage half, each with its last-known value and its independent stale
// flag. Service and provider labels are external source data carried raw; React
// escapes them.
type Data struct {
	Spend      *oci.Spend `json:"spend"`
	SpendStale bool       `json:"spendStale"`
	// SpendUpdatedAt is when the spend half last refreshed successfully. Because
	// spend polls on a slow cadence, the panel derives its age (now − this) to show
	// how old the served figure is: the stale flag says "old", this says how old.
	// Zero until the first successful read.
	SpendUpdatedAt time.Time            `json:"spendUpdatedAt"`
	Usage          *goapi.ProviderUsage `json:"usage"`
	UsageStale     bool                 `json:"usageStale"`
}

// Snapshot builds the cost envelope from both halves. The two sources degrade
// independently: the panel is source_down only when BOTH are currently
// unreachable, stale when one is (or a half has no value yet), and live when both
// are fresh. UpdatedAt is the more recent of the two halves' last successful read.
func (b *Bucket) Snapshot() core.Snapshot {
	b.mu.RLock()
	spend, spendStale, spendUpdated := b.lastSpend, b.spendStale, b.spendUpdated
	usage, usageStale, usageUpdated := b.lastUsage, b.usageStale, b.usageUpdated
	usageReason := b.usageReason
	b.mu.RUnlock()

	severity, headline := costHealth(spend, usage)
	return core.Snapshot{
		ID:        b.Meta().ID,
		Title:     b.Meta().Title,
		State:     costState(spend, spendStale, usage, usageStale),
		Reason:    usageReason,
		Severity:  severity,
		Headline:  headline,
		UpdatedAt: laterTime(spendUpdated, usageUpdated),
		Data: core.MarshalData(Data{
			Spend:          spend,
			SpendStale:     spendStale,
			SpendUpdatedAt: spendUpdated,
			Usage:          usage,
			UsageStale:     usageStale,
		}),
	}
}

// laterTime returns the more recent of two instants — the bucket's overall
// UpdatedAt is whichever half refreshed last.
func laterTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// costState derives the panel state from the two independent halves. Both sources
// unreachable (or one unreachable and the other never mirrored) is source_down;
// either half stale or not-yet-mirrored is stale; both fresh is live.
func costState(spend *oci.Spend, spendStale bool, usage *goapi.ProviderUsage, usageStale bool) core.State {
	spendDown := spendStale || spend == nil
	usageDown := usageStale || usage == nil
	switch {
	case spendStale && usageStale:
		return core.StateSourceDown
	case spendDown || usageDown:
		return core.StateStale
	default:
		return core.StateLive
	}
}

// costHealth grades what the money is buying. Spend alone cannot be graded — no
// budget is configured anywhere, and a number with nothing to exceed is not a
// health signal — so it only supplies the headline. The provider-usage half IS
// gradeable, from go-api's own outcome classification rather than any tuned
// threshold: a provider returning nothing but failures is a broken integration
// Altune is still paying for.
func costHealth(spend *oci.Spend, usage *goapi.ProviderUsage) (core.Severity, string) {
	return providerSeverity(usage), costHeadline(spend, usage)
}

// providerSeverity grades the worst provider. A provider with no observed calls
// is not graded: silence is not a failure, and go-api may simply not have used it.
func providerSeverity(usage *goapi.ProviderUsage) core.Severity {
	if usage == nil {
		return core.SeverityOK
	}
	worst := core.SeverityOK
	for _, outcomes := range *usage {
		if grade := providerGrade(outcomes); grade.Worse(worst) {
			worst = grade
		}
	}
	return worst
}

// providerGrade grades one provider by comparing its own counters — no tuned
// constant to drift: every call failing is a dead integration, more failing than
// succeeding is one degrading.
func providerGrade(o goapi.ProviderOutcomes) core.Severity {
	total := o.Total()
	if total == 0 {
		return core.SeverityOK
	}
	failures := total - o.OK
	switch {
	case o.OK == 0:
		return core.SeverityCritical
	case failures > o.OK:
		return core.SeverityWarn
	default:
		return core.SeverityOK
	}
}

// costHeadline names both halves' money figures, because the bucket's whole shape
// is two independent sources and either alone is half the answer.
func costHeadline(spend *oci.Spend, usage *goapi.ProviderUsage) string {
	parts := make([]string, 0, 2)
	if spend != nil {
		parts = append(parts, spendText(*spend))
	}
	if calls := totalCalls(usage); calls > 0 {
		parts = append(parts, fmt.Sprintf("%d provider calls", calls))
	}
	if len(parts) == 0 {
		return "no spend or provider usage yet"
	}
	return strings.Join(parts, " · ")
}

// spendText renders the month-to-date total. A missing currency (dev, an
// unconfigured OCI) drops the code rather than printing a dangling space.
func spendText(s oci.Spend) string {
	if code := strings.TrimSpace(s.Currency); code != "" {
		return fmt.Sprintf("%.2f %s month-to-date", s.Amount, code)
	}
	return fmt.Sprintf("%.2f month-to-date", s.Amount)
}

// totalCalls sums observed calls across every provider. The sum saturates rather
// than wrapping, matching ProviderOutcomes.Total's own discipline: a hostile or
// corrupt go-api with counters near the ceiling would otherwise wrap the total
// negative, and a negative total reads as "no calls" — silently hiding the half.
func totalCalls(usage *goapi.ProviderUsage) int64 {
	if usage == nil {
		return 0
	}
	var total int64
	for _, outcomes := range *usage {
		next := total + outcomes.Total()
		if next < total {
			return math.MaxInt64
		}
		total = next
	}
	return total
}

// recordSpend stores the latest spend snapshot and clears its stale flag. The
// snapshot is copied to the heap and only ever replaced (never mutated in place),
// so Render may read the pointer under the lock and use it after.
func (b *Bucket) recordSpend(s oci.Spend) {
	b.mu.Lock()
	baseline, haveBaseline := b.spendBaseline, b.haveSpendBaseline
	unchanged := haveBaseline && sameSpendReading(baseline, s)
	b.lastSpend = &s
	b.spendStale = false
	b.spendUpdated = time.Now().UTC()
	b.spendBaseline = s
	b.haveSpendBaseline = true
	b.mu.Unlock()

	if unchanged {
		return
	}
	b.recordSpendSeries(s, baseline, haveBaseline)
}

func (b *Bucket) recordSpendSeries(s, baseline oci.Spend, haveBaseline bool) {
	at := time.Now().UTC()
	b.series.Record(bucketID, seriesSpendMonthToDate, core.Point{At: at, Value: s.Amount})
	b.series.Record(bucketID, seriesSpendDaily, core.Point{At: at, Value: spendDailyDelta(s, baseline, haveBaseline)})
}

func spendDailyDelta(s, baseline oci.Spend, haveBaseline bool) float64 {
	if !haveBaseline || !s.PeriodStart.Equal(baseline.PeriodStart) {
		return s.Amount
	}
	if delta := s.Amount - baseline.Amount; delta >= 0 {
		return delta
	}
	return 0
}

func sameSpendReading(a, b oci.Spend) bool {
	return a.Amount == b.Amount && a.Currency == b.Currency &&
		a.PeriodStart.Equal(b.PeriodStart) && a.PeriodEnd.Equal(b.PeriodEnd)
}

func (b *Bucket) recordProviderSeries(u goapi.ProviderUsage) {
	at := time.Now().UTC()
	for provider, outcomes := range u {
		b.series.Record(bucketID, providerCallsSeriesStem+provider, core.Point{At: at, Value: float64(outcomes.Total())})
	}
}

// recordUsage stores the latest provider-usage snapshot and clears its stale flag,
// with the same replace-never-mutate discipline as recordSpend.
func (b *Bucket) recordUsage(u goapi.ProviderUsage) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastUsage = &u
	b.usageStale = false
	b.usageReason = ""
	b.usageUpdated = time.Now().UTC()
}

// markSpendStale flags the spend half stale while preserving the last-known
// snapshot — degrade-don't-crash: serve last-known flagged stale rather than
// dropping the panel.
func (b *Bucket) markSpendStale() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.spendStale = true
}

// markUsageStale flags the provider-usage half stale while preserving its
// last-known snapshot.
func (b *Bucket) markUsageStale(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.usageStale = true
	b.usageReason = goapi.Classify(err)
}

// spendReaderFromEnv builds the OCI spend reader. It is off by default: the
// usage-api needs an instance principal, which only exists on the OCI box, so dev
// and CI degrade to source-down rather than blocking on a metadata handshake that
// will never succeed. Set OVERSEER_OCI_ENABLED=1 on the deployed instance to
// activate it. Config normally lives in the config package, which this leaf may
// not edit; reading the one OCI knob here keeps the change within the bucket,
// matching the go-api-backed buckets.
func spendReaderFromEnv() spendReader {
	if !ociEnabled() {
		return nullSpendReader{}
	}
	// Build lazily on first Collect: InstancePrincipalConfigurationProvider does a
	// metadata handshake that can block, and Collect runs on the background ticker,
	// off the startup and HTTP-serve paths. A build failure degrades to stale and
	// is retried on the next tick, so a transient metadata hiccup self-heals.
	return &lazyReader{build: func() (spendReader, error) { return oci.NewFromInstancePrincipal() }}
}

// usageReaderFromEnv builds the read-only go-api client from OVERSEER_GOAPI_URL
// and the process-wide operator token source. Missing or invalid config yields a
// null client so an unconfigured provider-usage half degrades to source-down
// instead of failing the whole service at startup. Config normally lives in the
// config package, which this leaf may not edit; reading the go-api URL here and
// taking the credential from goapi.SharedTokenSource keeps the change within the
// bucket, matching the domain-quality, reliability and usage buckets.
func usageReaderFromEnv() usageReader {
	base := strings.TrimSpace(os.Getenv("OVERSEER_GOAPI_URL"))
	if base == "" {
		return nullUsageReader{}
	}
	c, err := goapi.New(base, goapi.SharedTokenSource())
	if err != nil {
		// Degrade to source-down, but say why: without this a URL typo is
		// indistinguishable from go-api being genuinely down (a permanently-STALE
		// half with no diagnostic).
		slog.Warn("cost: invalid OVERSEER_GOAPI_URL, degrading provider-usage to source-down", "error", err)
		return nullUsageReader{}
	}
	return c
}

// ociEnabled reports whether OCI spend reads are switched on for this deployment.
func ociEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("OVERSEER_OCI_ENABLED"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// spendIntervalFromEnv reads the tunable spend-refresh cadence, defaulting to
// hourly. config.Load already rejects a malformed value at startup; this second
// read keeps the bucket self-contained (matching the security bucket), and a bad
// value here still falls back to the default rather than disabling the refresh.
func spendIntervalFromEnv() time.Duration {
	raw := strings.TrimSpace(os.Getenv(spendIntervalEnvKey))
	if raw == "" {
		return defaultSpendInterval
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		slog.Warn("cost: invalid "+spendIntervalEnvKey+", using default",
			"value", raw, "default", defaultSpendInterval)
		return defaultSpendInterval
	}
	return d
}

// spendScheduler refreshes the OCI billing-spend half on its own low-frequency
// ticker, independent of the shell's 5s collect cadence — a metered, slow-moving
// month-to-date figure does not need reading every tick. It is recover-guarded (a
// panicking read never crashes the shell) and ctx-bound (it returns on
// cancellation, leaking no goroutine past the app's lifetime), following the
// security bucket's scheduler.
type spendScheduler struct {
	interval time.Duration
	refresh  func(context.Context)
}

// newSpendScheduler builds a scheduler. A non-positive interval is clamped to the
// default so the ticker can never be disabled or panic.
func newSpendScheduler(interval time.Duration, refresh func(context.Context)) *spendScheduler {
	if interval <= 0 {
		interval = defaultSpendInterval
	}
	return &spendScheduler{interval: interval, refresh: refresh}
}

// run refreshes spend once immediately (so the first render after startup has a
// figure), then on every tick until ctx is done. It is the single background
// goroutine the bucket owns and it returns on cancellation, so nothing leaks.
func (s *spendScheduler) run(ctx context.Context) {
	s.safeRefreshOnce(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.safeRefreshOnce(ctx)
		}
	}
}

// safeRefreshOnce refreshes spend with a recover, containing any panic so a single
// misbehaving read can neither crash the process nor kill the scheduler: the ticker
// survives and the next refresh executes. run() is a long-lived background
// goroutine outside the shell's safeCollect recover, so containment lives here.
func (s *spendScheduler) safeRefreshOnce(ctx context.Context) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.ErrorContext(ctx, "cost: spend refresh panicked", "recover", rec)
		}
	}()
	s.refresh(ctx)
}

// lazyReader defers building the real OCI client until the first read, so the
// blocking instance-principal handshake never runs on the startup path. A failed
// build degrades to source-down and is retried next tick.
type lazyReader struct {
	build func() (spendReader, error)

	mu     sync.Mutex
	client spendReader
}

func (l *lazyReader) CurrentPeriodSpend(ctx context.Context) (oci.Spend, error) {
	client, err := l.ensure()
	if err != nil {
		return oci.Spend{}, &oci.SourceDownError{Err: err}
	}
	return client.CurrentPeriodSpend(ctx)
}

// ensure builds the client once and caches it, returning the build error until a
// build succeeds. It holds the lock only around the cached pointer.
func (l *lazyReader) ensure() (spendReader, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.client != nil {
		return l.client, nil
	}
	client, err := l.build()
	if err != nil {
		return nil, err
	}
	l.client = client
	return client, nil
}

// nullSpendReader stands in when OCI reads are disabled: every read reports
// source-down, so the spend half renders stale rather than nil-panicking.
type nullSpendReader struct{}

func (nullSpendReader) CurrentPeriodSpend(context.Context) (oci.Spend, error) {
	return oci.Spend{}, &oci.SourceDownError{Err: errUnconfigured}
}

// nullUsageReader stands in when go-api is unconfigured: every read reports
// source-down, so the provider-usage half renders stale rather than nil-panicking.
type nullUsageReader struct{}

func (nullUsageReader) AdminProviderUsage(context.Context) (goapi.ProviderUsage, error) {
	return nil, &goapi.SourceDownError{Op: "GET /observe/metrics/live", Err: errUsageUnconfigured}
}

type discardSeries struct{}

func (discardSeries) Record(string, string, core.Point) {}

func (discardSeries) Query(string, string, time.Time, time.Time) ([]core.Point, error) {
	return nil, nil
}

func init() { core.Register(New()) }
