// Package cost is the Overseer's Cost bucket: at a glance, what is Altune
// spending. The brief renders two independent signals — provider API usage (from
// go-api's cost-enabler) and OCI infra spend (from OCI's usage-api) — each
// degrading on its own, like the domain-quality bucket. This leaf builds the
// OCI infra-spend half; the provider-usage half is blocked on the cost-enabler
// epic and slots in as a second independent source later.
//
// The OCI spend is read through a read-only usage-api client authenticated by
// instance principal (internal/oci). The bucket depends only on the UsageClient
// seam, never the OCI SDK, so a fake drives it in tests with no OCI auth. Spend
// carries no OCI identifier by construction, so nothing OCI-identifying reaches
// the panel or the logs; history is bounded by a ring; and an unreachable
// usage-api degrades to the last-known spend flagged STALE rather than going dark.
// The bucket owns all its own files and self-registers with one blank import in
// the composition root (the additive-buckets invariant).
package cost

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/oci"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// historyCapacity bounds the retained spend samples. The ring caps memory by
// construction no matter how long the service runs.
const historyCapacity = 120

// errUnconfigured is the error the null client reports when OCI reads are not
// enabled: the spend half renders stale rather than the whole service failing at
// startup. Off an OCI instance (dev, CI) there is no instance principal, so this
// is the normal state until the bucket runs on the real box.
var errUnconfigured = errors.New("cost: OCI usage-api not enabled")

// spendReader is the seam onto the OCI usage-api read the bucket needs — the one
// method from oci.UsageClient. Depending on this interface (not oci.Client) lets a
// test drive spend deterministically, including the degrade path, with no OCI auth.
type spendReader interface {
	CurrentPeriodSpend(ctx context.Context) (oci.Spend, error)
}

// Bucket reads OCI's current-period infra spend and renders it, flagging the panel
// STALE when the usage-api is currently unreachable while preserving the last-known
// figure. A bounded ring keeps a short spend trend.
type Bucket struct {
	spend   spendReader
	history core.Store

	// mu guards the last-known spend snapshot and its stale flag, which the collect
	// loop writes and the HTTP render reads.
	mu    sync.RWMutex
	last  *oci.Spend
	stale bool
}

// New builds the Cost bucket from the environment. When OCI reads are not enabled
// it falls back to a null client: the bucket still registers and renders stale
// rather than crashing.
func New() *Bucket { return newBucket(readerFromEnv()) }

// newBucket is the injectable constructor tests use to supply a controllable
// reader; production goes through New.
func newBucket(r spendReader) *Bucket {
	return &Bucket{spend: r, history: core.NewRingStore(historyCapacity)}
}

func (b *Bucket) Meta() core.Meta {
	return core.Meta{ID: "cost", Title: "Cost"}
}

// Collect reads the current-period OCI spend. On success it records the fresh
// snapshot and returns a bounded spend signal; when the usage-api is unreachable it
// flags the view stale and returns the error, so the shell keeps the last-known
// panel and Render marks it STALE.
func (b *Bucket) Collect(ctx context.Context) ([]core.Signal, error) {
	spend, err := b.spend.CurrentPeriodSpend(ctx)
	if err != nil {
		b.markStale()
		return nil, fmt.Errorf("cost: oci spend unreachable: %w", err)
	}
	b.recordFresh(spend)
	return []core.Signal{spendSignal(spend)}, nil
}

func (b *Bucket) Store(signals []core.Signal) {
	for _, s := range signals {
		b.history.Add(s)
	}
}

// Render builds the panel from the last-known spend snapshot (flagged stale when
// the read is currently unreachable) and the bounded spend trend.
func (b *Bucket) Render() core.Panel {
	b.mu.RLock()
	last := b.last
	stale := b.stale
	b.mu.RUnlock()

	return core.Panel{Title: b.Meta().Title, Body: renderBody(last, stale, b.history.Snapshot())}
}

// recordFresh stores the latest spend snapshot and clears the stale flag. The
// snapshot is copied to the heap and only ever replaced (never mutated in place),
// so Render may read the pointer under the lock and use it after.
func (b *Bucket) recordFresh(s oci.Spend) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.last = &s
	b.stale = false
}

// markStale flags the view stale while preserving the last-known snapshot — the
// degrade-don't-crash behaviour: serve last-known flagged stale rather than
// dropping the panel.
func (b *Bucket) markStale() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stale = true
}

// spendSignal captures the period total for the bounded spend trend. The text is
// built only from the total, currency and service count — never an OCI identifier —
// and is HTML-escaped at render time.
func spendSignal(s oci.Spend) core.Signal {
	return core.Signal{
		At:   time.Now().UTC(),
		Kind: "spend",
		Text: fmt.Sprintf("OCI spend %.2f %s across %d service(s) (month-to-date)", s.Amount, s.Currency, len(s.Lines)),
	}
}

// readerFromEnv builds the OCI spend reader. It is off by default: the usage-api
// needs an instance principal, which only exists on the OCI box, so dev and CI
// degrade to source-down rather than blocking on a metadata handshake that will
// never succeed. Set OVERSEER_OCI_ENABLED=1 on the deployed instance to activate
// it. Config normally lives in the config package, which this leaf may not edit;
// reading the one OCI knob here keeps the change within the bucket, matching the
// go-api-backed buckets.
func readerFromEnv() spendReader {
	if !ociEnabled() {
		return nullReader{}
	}
	// Build lazily on first Collect: InstancePrincipalConfigurationProvider does a
	// metadata handshake that can block, and Collect runs on the background ticker,
	// off the startup and HTTP-serve paths. A build failure degrades to stale and
	// is retried on the next tick, so a transient metadata hiccup self-heals.
	return &lazyReader{build: func() (spendReader, error) { return oci.NewFromInstancePrincipal() }}
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

// nullReader stands in when OCI reads are disabled: every read reports source-down,
// so the bucket renders stale rather than nil-panicking.
type nullReader struct{}

func (nullReader) CurrentPeriodSpend(context.Context) (oci.Spend, error) {
	return oci.Spend{}, &oci.SourceDownError{Err: errUnconfigured}
}

func init() { core.Register(New()) }
