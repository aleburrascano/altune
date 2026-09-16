// Package cost is the Overseer's Cost bucket: at a glance, what is Altune
// spending. The brief renders two independent signals — provider API usage (from
// go-api's cost-enabler) and OCI infra spend (from OCI's usage-api) — each
// degrading on its own, like the domain-quality bucket. This bucket builds both
// halves: the OCI infra-spend half and the provider-usage half.
//
// The OCI spend is read through a read-only usage-api client authenticated by
// instance principal (internal/oci). The provider usage is read through the
// read-only go-api client (internal/goapi), operator-authenticated, off the
// operator /admin/metrics/live "providers" field. The bucket depends only on the
// two read seams (spendReader, usageReader), never the OCI SDK or a live go-api,
// so fakes drive both in tests with no external auth. Neither source carries an
// identifier that must not leak (spend carries no OCI identifier; provider counts
// carry only bounded provider labels), so nothing sensitive reaches the panel or
// logs; and an unreachable source degrades to its last-known value flagged STALE
// rather than going dark. The two sources degrade
// INDEPENDENTLY: one source down flags only its own half stale, never the other.
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

// errBothDown is returned by Collect only when BOTH source reads are unreachable
// and nothing fresh arrived, so the shell logs a genuine outage while Render keeps
// serving each half's last-known value flagged stale. A single source down never
// returns an error — that half degrades independently and the other stays live.
var errBothDown = errors.New("cost: oci spend and provider usage both unreachable")

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
	spend spendReader
	usage usageReader

	// mu guards the last-known snapshots and their stale flags, which the collect
	// loop writes and the HTTP render reads.
	mu         sync.RWMutex
	lastSpend  *oci.Spend
	spendStale bool
	lastUsage  *goapi.ProviderUsage
	usageStale bool
	updated    time.Time
}

// New builds the Cost bucket from the environment. When OCI reads are not enabled
// or go-api is not configured it falls back to null clients: the bucket still
// registers and each half renders stale rather than crashing.
func New() *Bucket { return newBucket(spendReaderFromEnv(), usageReaderFromEnv()) }

// newBucket is the injectable constructor tests use to supply controllable
// readers; production goes through New.
func newBucket(spend spendReader, usage usageReader) *Bucket {
	return &Bucket{spend: spend, usage: usage}
}

func (b *Bucket) Meta() core.Meta {
	return core.Meta{ID: "cost", Title: "Cost"}
}

// Collect reads both sources. Each half records its fresh snapshot on success or
// is flagged stale on failure while its last-known value is preserved — the two
// are INDEPENDENT, so an OCI read failing never disturbs a working provider-usage
// read and vice versa. Only when BOTH reads are unreachable does Collect return an
// error, so the shell logs a genuine outage but never suppresses a half-live panel.
func (b *Bucket) Collect(ctx context.Context) ([]core.Signal, error) {
	spend, spendErr := b.spend.CurrentPeriodSpend(ctx)
	if spendErr != nil {
		b.markSpendStale()
	} else {
		b.recordSpend(spend)
	}

	usage, usageErr := b.usage.AdminProviderUsage(ctx)
	if usageErr != nil {
		b.markUsageStale()
	} else {
		b.recordUsage(usage)
	}

	if spendErr != nil && usageErr != nil {
		return nil, fmt.Errorf("%w: oci=%w providers=%w", errBothDown, spendErr, usageErr)
	}
	return nil, nil
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
	Spend      *oci.Spend           `json:"spend"`
	SpendStale bool                 `json:"spendStale"`
	Usage      *goapi.ProviderUsage `json:"usage"`
	UsageStale bool                 `json:"usageStale"`
}

// Snapshot builds the cost envelope from both halves. The two sources degrade
// independently: the panel is source_down only when BOTH are currently
// unreachable, stale when one is (or a half has no value yet), and live when both
// are fresh. UpdatedAt is the more recent of the two halves' last successful read.
func (b *Bucket) Snapshot() core.Snapshot {
	b.mu.RLock()
	spend, spendStale := b.lastSpend, b.spendStale
	usage, usageStale := b.lastUsage, b.usageStale
	updated := b.updated
	b.mu.RUnlock()

	return core.Snapshot{
		ID:        b.Meta().ID,
		Title:     b.Meta().Title,
		State:     costState(spend, spendStale, usage, usageStale),
		UpdatedAt: updated,
		Data: core.MarshalData(Data{
			Spend:      spend,
			SpendStale: spendStale,
			Usage:      usage,
			UsageStale: usageStale,
		}),
	}
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

// recordSpend stores the latest spend snapshot and clears its stale flag. The
// snapshot is copied to the heap and only ever replaced (never mutated in place),
// so Render may read the pointer under the lock and use it after.
func (b *Bucket) recordSpend(s oci.Spend) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastSpend = &s
	b.spendStale = false
	b.updated = time.Now().UTC()
}

// recordUsage stores the latest provider-usage snapshot and clears its stale flag,
// with the same replace-never-mutate discipline as recordSpend.
func (b *Bucket) recordUsage(u goapi.ProviderUsage) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastUsage = &u
	b.usageStale = false
	b.updated = time.Now().UTC()
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
func (b *Bucket) markUsageStale() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.usageStale = true
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
	return nil, &goapi.SourceDownError{Op: "GET /admin/metrics/live", Err: errUsageUnconfigured}
}

func init() { core.Register(New()) }
