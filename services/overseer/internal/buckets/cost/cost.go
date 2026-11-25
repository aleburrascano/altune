package cost

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"altune/overseer/internal/oci"
	"altune/overseer/internal/panicguard"
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
)

var errUnconfigured = errors.New("cost: OCI usage-api not enabled")

var errUsageUnconfigured = errors.New("cost: go-api not configured")

var ErrBothDown = errors.New("cost: oci spend and provider usage both unreachable")

const (
	defaultSpendInterval = time.Hour

	bucketID                = "cost"
	seriesSpendDaily        = "spend_daily"
	seriesSpendMonthToDate  = "spend_month_to_date"
	providerCallsSeriesStem = "provider_calls:"
)

type SpendReader interface {
	CurrentPeriodSpend(ctx context.Context) (oci.Spend, error)
}

type usageReader interface {
	AdminProviderUsage(ctx context.Context) (goapi.ProviderUsage, error)
}

type Bucket struct {
	spend      SpendReader
	usage      usageReader
	spendSched *spendScheduler
	start      panicguard.Once
	series     core.Series

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

type Deps struct {
	ConnectSpend  func() (SpendReader, error)
	Usage         usageReader
	SpendInterval time.Duration
}

func New(deps Deps) *Bucket {
	b := &Bucket{spend: nullSpendReader{}, usage: deps.Usage, series: core.DiscardSeries{}}
	if deps.ConnectSpend != nil {
		b.spend = &lazyReader{build: deps.ConnectSpend}
	}
	if b.usage == nil {
		b.usage = nullUsageReader{}
	}
	b.spendSched = newSpendScheduler(deps.SpendInterval, b.refreshSpend)
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

func (b *Bucket) Start(ctx context.Context) {
	b.start.Contain(ctx, "cost: spend scheduler panicked", func() { b.spendSched.run(ctx) })
}

func (b *Bucket) Collect(ctx context.Context) ([]core.Signal, error) {
	usage, usageErr := b.usage.AdminProviderUsage(ctx)
	if usageErr != nil {
		b.markUsageStale(usageErr)
	} else {
		b.recordUsage(usage)
		b.recordProviderSeries(usage)
	}

	if usageErr != nil && b.spendUnreachable() {
		return nil, fmt.Errorf("%w: providers=%w (spend half also unreachable)", ErrBothDown, usageErr)
	}
	return nil, nil
}

func (b *Bucket) refreshSpend(ctx context.Context) {
	spend, err := b.spend.CurrentPeriodSpend(ctx)
	if err != nil {
		b.markSpendStale()
		return
	}
	b.recordSpend(spend)
}

func (b *Bucket) spendUnreachable() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.spendStale || b.lastSpend == nil
}

func (b *Bucket) Store([]core.Signal) {}

type Data struct {
	Spend          *oci.Spend           `json:"spend"`
	SpendStale     bool                 `json:"spendStale"`
	SpendUpdatedAt time.Time            `json:"spendUpdatedAt"`
	Usage          *goapi.ProviderUsage `json:"usage"`
	UsageStale     bool                 `json:"usageStale"`
}

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

func laterTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

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

func costHealth(spend *oci.Spend, usage *goapi.ProviderUsage) (core.Severity, string) {
	return providerSeverity(usage), costHeadline(spend, usage)
}

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

func spendText(s oci.Spend) string {
	if code := strings.TrimSpace(s.Currency); code != "" {
		return fmt.Sprintf("%.2f %s month-to-date", s.Amount, code)
	}
	return fmt.Sprintf("%.2f month-to-date", s.Amount)
}

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

func (b *Bucket) recordUsage(u goapi.ProviderUsage) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastUsage = &u
	b.usageStale = false
	b.usageReason = ""
	b.usageUpdated = time.Now().UTC()
}

func (b *Bucket) markSpendStale() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.spendStale = true
}

func (b *Bucket) markUsageStale(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.usageStale = true
	b.usageReason = goapi.Classify(err)
}

type spendScheduler struct {
	interval time.Duration
	refresh  func(context.Context)
}

func newSpendScheduler(interval time.Duration, refresh func(context.Context)) *spendScheduler {
	if interval <= 0 {
		interval = defaultSpendInterval
	}
	return &spendScheduler{interval: interval, refresh: refresh}
}

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

func (s *spendScheduler) safeRefreshOnce(ctx context.Context) {
	panicguard.Contain(ctx, "cost: spend refresh panicked", func() { s.refresh(ctx) })
}

type lazyReader struct {
	build func() (SpendReader, error)

	mu     sync.Mutex
	client SpendReader
}

func (l *lazyReader) CurrentPeriodSpend(ctx context.Context) (oci.Spend, error) {
	client, err := l.ensure()
	if err != nil {
		return oci.Spend{}, &oci.SourceDownError{Err: err}
	}
	return client.CurrentPeriodSpend(ctx)
}

func (l *lazyReader) ensure() (SpendReader, error) {
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

type nullSpendReader struct{}

func (nullSpendReader) CurrentPeriodSpend(context.Context) (oci.Spend, error) {
	return oci.Spend{}, &oci.SourceDownError{Err: errUnconfigured}
}

type nullUsageReader struct{}

func (nullUsageReader) AdminProviderUsage(context.Context) (goapi.ProviderUsage, error) {
	return nil, &goapi.SourceDownError{Op: "GET /observe/metrics/live", Err: errUsageUnconfigured}
}
