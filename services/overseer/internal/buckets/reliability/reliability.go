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
	historyCapacity     = 120
	pollCapacity        = 120
	defaultPollInterval = 30 * time.Second

	bucketID        = "reliability"
	seriesUp        = "up"
	seriesLatencyMS = "latency_ms"
)

var errUnconfigured = errors.New("reliability: go-api not configured")

type healthReader interface {
	AdminHealth(ctx context.Context) (goapi.OperatorHealth, error)
}

type Bucket struct {
	reader  healthReader
	poller  *reachPoller
	history *core.RingStore

	mu          sync.RWMutex
	lastHealth  *goapi.OperatorHealth
	adminStale  bool
	adminReason string

	start   sync.Once
	running sync.WaitGroup
}

func New() *Bucket {
	reader, checker := clientFromEnv()
	return newBucket(reader, checker, pollIntervalFromEnv())
}

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

func (b *Bucket) Start(ctx context.Context) {
	b.start.Do(func() { b.running.Go(func() { b.poller.run(ctx) }) })
}

func (b *Bucket) Wait() {
	b.running.Wait()
}

func (b *Bucket) Rings() map[string]*core.RingStore {
	return map[string]*core.RingStore{"history": b.history, "poll": b.poller.samples}
}

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

type Data struct {
	Reachability string                `json:"reachability"`
	Health       *goapi.OperatorHealth `json:"health"`
	AdminStale   bool                  `json:"adminStale"`
	History      []core.Signal         `json:"history"`
	Poll         []core.Signal         `json:"poll"`
}

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

func uptimeText(poll []core.Signal) string {
	if len(poll) == 0 {
		return "no reachability probes yet"
	}
	return fmt.Sprintf("uptime %.1f%%", float64(upProbes(poll))/float64(len(poll))*100)
}

func hasFailedProbe(poll []core.Signal) bool { return upProbes(poll) < len(poll) }

func upProbes(poll []core.Signal) int {
	up := 0
	for _, s := range poll {
		if s.Text == goapi.StatusUp.String() {
			up++
		}
	}
	return up
}

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

func (b *Bucket) recordFresh(h goapi.OperatorHealth) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastHealth = &h
	b.adminStale = false
	b.adminReason = ""
}

func (b *Bucket) markAdminStale(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.adminStale = true
	b.adminReason = goapi.Classify(err)
}

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

func clientFromEnv() (healthReader, reachChecker) {
	base := strings.TrimSpace(os.Getenv("OVERSEER_GOAPI_URL"))
	if base == "" {
		n := nullClient{}
		return n, n
	}
	c, err := goapi.New(base, goapi.SharedTokenSource())
	if err != nil {
		slog.Warn("reliability: invalid OVERSEER_GOAPI_URL, degrading to source-down", "error", err)
		n := nullClient{}
		return n, n
	}
	return c, c
}

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

type nullClient struct{}

func (nullClient) AdminHealth(context.Context) (goapi.OperatorHealth, error) {
	return goapi.OperatorHealth{}, &goapi.SourceDownError{Op: "GET /observe/health", Err: errUnconfigured}
}

func (nullClient) Health(context.Context) (goapi.Health, error) {
	return goapi.Health{}, &goapi.SourceDownError{Op: "GET /health", Err: errUnconfigured}
}

func init() { core.Register(New()) }
