package logs

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

const logCapacity = 200

var errSourceDown = errors.New("logs: go-api log source unreachable")

type source interface {
	Run(ctx context.Context) error
	Records() <-chan goapi.LogRecord
	Status() goapi.Status
}

type Bucket struct {
	records  core.Store
	src      source
	minLevel string
	start    sync.Once
}

func New() *Bucket {
	return newBucket(sourceFromEnv(), minLevelFromEnv())
}

func newBucket(src source, minLevel string) *Bucket {
	return &Bucket{
		records:  core.NewRingStore(logCapacity),
		src:      src,
		minLevel: minLevel,
	}
}

func (b *Bucket) Meta() core.Meta {
	return core.Meta{ID: "logs", Title: "Logs"}
}

func (b *Bucket) Start(ctx context.Context) {
	b.start.Do(func() { go b.runSource(ctx) })
}

func (b *Bucket) Collect(context.Context) ([]core.Signal, error) {
	signals := b.drain()
	if len(signals) == 0 && b.src.Status() == goapi.StatusDown {
		return nil, errSourceDown
	}
	return signals, nil
}

func (b *Bucket) runSource(ctx context.Context) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.ErrorContext(ctx, "logs: source pump panicked", "recover", rec)
		}
	}()
	_ = b.src.Run(ctx)
}

func (b *Bucket) drain() []core.Signal {
	var signals []core.Signal
	for _, rec := range goapi.DrainPending(b.src, b.src.Records()) {
		signals = append(signals, toSignal(rec))
	}
	return signals
}

func (b *Bucket) Store(signals []core.Signal) {
	for _, s := range signals {
		b.records.Add(s)
	}
}

type Data struct {
	Records  []goapi.LogRecord `json:"records"`
	MinLevel string            `json:"minLevel"`
	Dropped  int               `json:"dropped"`
}

func (b *Bucket) Snapshot() core.Snapshot {
	records := filteredRecords(b.records.Snapshot(), b.minLevel)
	updated := time.Time{}
	if n := len(records); n > 0 {
		updated = records[n-1].Time
	}
	severity, headline := logsHealth(records)
	status, failure := goapi.StreamStatus(b.src)
	return core.Snapshot{
		ID:        b.Meta().ID,
		Title:     b.Meta().Title,
		State:     core.State(status.PanelState()),
		Reason:    status.PanelReason(failure),
		Severity:  severity,
		Headline:  headline,
		UpdatedAt: updated,
		Data:      core.MarshalData(Data{Records: records, MinLevel: effectiveLevel(b.minLevel), Dropped: goapi.TotalDropped(b.src, b.records.Dropped())}),
	}
}

func toSignal(rec goapi.LogRecord) core.Signal {
	payload, err := json.Marshal(redactSensitiveFields(rec))
	if err != nil {
		payload = []byte("{}")
	}
	return core.Signal{At: rec.Time, Kind: normalizeLevel(rec.Level), Text: string(payload)}
}

const redactedValue = "[REDACTED]"

var sensitiveKeyMarkers = []string{
	"token",
	"authorization",
	"password",
	"passwd",
	"secret",
	"email",
	"credential",
	"apikey",
	"accesskey",
	"privatekey",
	"bearer",
	"cookie",
}

func redactSensitiveFields(rec goapi.LogRecord) goapi.LogRecord {
	if len(rec.Fields) == 0 {
		return rec
	}
	redacted := make(map[string]string, len(rec.Fields))
	for key, value := range rec.Fields {
		if isSensitiveKey(key) {
			value = redactedValue
		}
		redacted[key] = value
	}
	rec.Fields = redacted
	return rec
}

func isSensitiveKey(name string) bool {
	normalized := normalizeKey(name)
	for _, marker := range sensitiveKeyMarkers {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func normalizeKey(name string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		default:
			return -1
		}
	}, strings.ToLower(name))
}

func decodeRecord(s core.Signal) goapi.LogRecord {
	var rec goapi.LogRecord
	if err := json.Unmarshal([]byte(s.Text), &rec); err != nil {
		return goapi.LogRecord{Time: s.At, Level: s.Kind}
	}
	if rec.Level == "" {
		rec.Level = s.Kind
	}
	return rec
}

func sourceFromEnv() source {
	base := strings.TrimSpace(os.Getenv("OVERSEER_GOAPI_URL"))
	if base == "" {
		return newNullSource()
	}
	c, err := goapi.NewLogsConsumer(base, goapi.SharedTokenSource())
	if err != nil {
		slog.Warn("logs: invalid OVERSEER_GOAPI_URL, degrading to source-down", "error", err)
		return newNullSource()
	}
	return c
}

func minLevelFromEnv() string {
	return strings.TrimSpace(os.Getenv("OVERSEER_LOGS_MIN_LEVEL"))
}

type nullSource struct{ records chan goapi.LogRecord }

func newNullSource() *nullSource { return &nullSource{records: make(chan goapi.LogRecord)} }

func (n *nullSource) Run(ctx context.Context) error   { <-ctx.Done(); return ctx.Err() }
func (n *nullSource) Records() <-chan goapi.LogRecord { return n.records }
func (n *nullSource) Status() goapi.Status            { return goapi.StatusDown }

func (n *nullSource) LastError() error {
	return &goapi.SourceDownError{Op: "stream", Err: errSourceDown}
}

func init() { core.Register(New()) }
