// Package logs is Overseer's live log-tail bucket: a bounded, filterable view of
// what the watched app is logging, always available. It consumes go-api's
// operator log SSE (via the goapi LogsConsumer — a second stream alongside the
// event stream) into a bounded ring and renders a level-filtered tail. When
// go-api is unreachable the consumer reports source-down and the bucket serves
// its last-known tail flagged STALE, so the shell never goes dark — the
// outlives-the-app invariant at a leaf. Every rendered log field is HTML-escaped
// because log text is watched-app data (the render-escaping spine invariant). It
// owns all its own files and self-registers with one blank import in the
// composition root (the additive-buckets invariant).
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

// logCapacity bounds the retained log tail. The ring caps memory by construction
// no matter how long the stream runs or how fast records arrive; the upstream log
// ring is itself bounded, so this is a bound on top of a bound.
const logCapacity = 200

// errSourceDown is returned by Collect when the log source is unreachable and
// nothing fresh arrived, so the shell logs it and skips the store while Render
// keeps serving the last-known tail flagged stale (the degrade path the Bucket
// contract prescribes).
var errSourceDown = errors.New("logs: go-api log source unreachable")

// source is the seam onto the log SSE consumer: the subset of *goapi.LogsConsumer
// the bucket needs. Depending on the interface, not the concrete consumer, lets a
// test inject a controllable source and drop it deterministically.
type source interface {
	Run(ctx context.Context) error
	Records() <-chan goapi.LogRecord
	Status() goapi.Status
}

// Bucket ingests go-api structured log records into a bounded ring and renders
// them as a level-filtered tail.
type Bucket struct {
	records  core.Store
	src      source
	minLevel string
	start    sync.Once
}

// New builds the Logs bucket from the environment. When go-api is not configured
// (no URL or token) it falls back to a null source: the bucket still registers,
// stays bounded and renders "source down" rather than crashing.
func New() *Bucket {
	return newBucket(sourceFromEnv(), minLevelFromEnv())
}

// newBucket is the injectable constructor tests use to supply a controllable
// source and a fixed minimum level; production goes through New.
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

// Start launches the SSE pump once, bound to the app-lifetime ctx the shell hands
// it — cancelled only at shutdown, so the pump survives the per-tick collect
// deadline (#1812) that froze it after one run when it was launched from Collect.
// The sync.Once makes a second Start a no-op, so the bucket owns exactly one pump
// goroutine however the shell drives it, and that goroutine exits when ctx is
// cancelled at shutdown so nothing leaks.
func (b *Bucket) Start(ctx context.Context) {
	b.start.Do(func() { go b.runSource(ctx) })
}

// Collect drains whatever records have arrived since the last cycle; the SSE pump
// that feeds them runs on the app-lifetime Start hook, not here. An unreachable
// source with nothing fresh returns errSourceDown so the shell keeps the
// last-known tail and Render flags it stale.
func (b *Bucket) Collect(context.Context) ([]core.Signal, error) {
	signals := b.drain()
	if len(signals) == 0 && b.src.Status() == goapi.StatusDown {
		return nil, errSourceDown
	}
	return signals, nil
}

// runSource runs the SSE pump in its own goroutine, containing any panic so a
// misbehaving source cannot crash the whole process (degrade-don't-crash on the
// bucket's background path).
func (b *Bucket) runSource(ctx context.Context) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.ErrorContext(ctx, "logs: source pump panicked", "recover", rec)
		}
	}()
	_ = b.src.Run(ctx)
}

// drain pulls every buffered record without blocking, converting each to a
// signal. It reads only what is already queued so a cycle never waits on the
// network.
func (b *Bucket) drain() []core.Signal {
	var signals []core.Signal
	for {
		select {
		case rec, ok := <-b.src.Records():
			if !ok {
				return signals
			}
			signals = append(signals, toSignal(rec))
		default:
			return signals
		}
	}
}

func (b *Bucket) Store(signals []core.Signal) {
	for _, s := range signals {
		b.records.Add(s)
	}
}

// Data is the logs panel payload: the level-filtered tail (oldest first) and the
// active minimum level. Every record field is watched-app data carried raw; React
// escapes it on render.
type Data struct {
	Records  []goapi.LogRecord `json:"records"`
	MinLevel string            `json:"minLevel"`
	// Dropped is how many older log records the ring has evicted under a burst.
	// The tail shows only the retained window; this makes the truncation visible
	// so an operator can tell a full window from a lossy one during an incident.
	Dropped int `json:"dropped"`
}

// Snapshot builds the logs envelope from the bounded tail. State follows the SSE
// consumer's status: an unreachable go-api is source_down while the last-known
// tail is still served, so the panel never goes dark. UpdatedAt is the newest
// retained record's timestamp.
func (b *Bucket) Snapshot() core.Snapshot {
	records := filteredRecords(b.records.Snapshot(), b.minLevel)
	updated := time.Time{}
	if n := len(records); n > 0 {
		updated = records[n-1].Time
	}
	severity, headline := logsHealth(records)
	status := b.src.Status()
	return core.Snapshot{
		ID:        b.Meta().ID,
		Title:     b.Meta().Title,
		State:     core.State(status.PanelState()),
		Reason:    status.PanelReason(goapi.StreamReason(b.src)),
		Severity:  severity,
		Headline:  headline,
		UpdatedAt: updated,
		Data:      core.MarshalData(Data{Records: records, MinLevel: effectiveLevel(b.minLevel), Dropped: b.records.Dropped()}),
	}
}

// toSignal packs one log record into the shared signal shape the RingStore holds.
// Sensitive attrs are redacted first: go-api logs its full attrs bag verbatim and
// overseer copies it into a broadly-viewable, persistent store, so a value under a
// denylisted key must never be stored (the ingest-side counterpart to go-api's own
// secret hygiene). The normalized level goes in Kind so the tail can filter by
// level without decoding; the surviving record (message + non-sensitive fields) is
// JSON-encoded into Text and stored raw — every stored part is HTML-escaped at
// render time, never here. Marshal cannot fail for this value (a time, three
// strings and a string map), but a belt-and-braces empty payload keeps a render
// decode total rather than panicky.
func toSignal(rec goapi.LogRecord) core.Signal {
	payload, err := json.Marshal(redactSensitiveFields(rec))
	if err != nil {
		payload = []byte("{}")
	}
	return core.Signal{At: rec.Time, Kind: normalizeLevel(rec.Level), Text: string(payload)}
}

// redactedValue replaces a denylisted attr's value. The key is kept so an operator
// still sees a sensitive field was present; the value never reaches the store.
const redactedValue = "[REDACTED]"

// sensitiveKeyMarkers is the denylist of attr-name fragments whose value is a
// credential or a personal identifier. It is a local constant rather than a shared
// import because go-api's internal/shared/redact lives in a separate Go module;
// the vocabulary is deliberately mirrored so a name masked there is masked here.
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

// redactSensitiveFields returns rec with every denylisted attr value masked; the
// message and non-sensitive attrs are untouched. It builds a fresh map rather than
// mutating the record's, so the source's value is never altered in place.
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

// isSensitiveKey reports whether an attr name holds a credential or personal
// identifier. It matches a normalized form of the name so case and separator
// variants — "Authorization", "access token", "api-key" — resolve to the same
// marker and cannot smuggle a value past the denylist.
func isSensitiveKey(name string) bool {
	normalized := normalizeKey(name)
	for _, marker := range sensitiveKeyMarkers {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

// normalizeKey lower-cases name and drops every character that is not an ASCII
// letter or digit, collapsing spacing and separator variants of a key to one form.
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

// decodeRecord reverses toSignal for rendering. A signal whose Text is not a
// decodable record still yields a usable record (its stored level plus an empty
// message), so one corrupt entry never breaks the tail.
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

// sourceFromEnv builds the log SSE consumer from OVERSEER_GOAPI_URL and the
// process-wide operator token source. Missing or invalid config yields a null
// source so the bucket degrades to "source down" instead of failing the whole
// service at startup. It reuses the same go-api URL and the shared
// goapi.SharedTokenSource the other buckets read.
func sourceFromEnv() source {
	base := strings.TrimSpace(os.Getenv("OVERSEER_GOAPI_URL"))
	if base == "" {
		return newNullSource()
	}
	c, err := goapi.NewLogsConsumer(base, goapi.SharedTokenSource())
	if err != nil {
		// Degrade to source-down, but say why: without this a URL typo is
		// indistinguishable from go-api being genuinely down (a permanently-STALE
		// panel with no diagnostic).
		slog.Warn("logs: invalid OVERSEER_GOAPI_URL, degrading to source-down", "error", err)
		return newNullSource()
	}
	return c
}

// minLevelFromEnv reads the tail's minimum level from OVERSEER_LOGS_MIN_LEVEL.
// An unset or unrecognized value shows everything (DEBUG and up), so the owner
// sees the full tail by default and narrows it deliberately.
func minLevelFromEnv() string {
	return strings.TrimSpace(os.Getenv("OVERSEER_LOGS_MIN_LEVEL"))
}

// nullSource stands in when go-api is unconfigured: it is permanently down and
// never delivers a record, so an unconfigured bucket renders stale and bounded
// rather than nil-panicking on Status/Records.
type nullSource struct{ records chan goapi.LogRecord }

func newNullSource() *nullSource { return &nullSource{records: make(chan goapi.LogRecord)} }

func (n *nullSource) Run(ctx context.Context) error   { <-ctx.Done(); return ctx.Err() }
func (n *nullSource) Records() <-chan goapi.LogRecord { return n.records }
func (n *nullSource) Status() goapi.Status            { return goapi.StatusDown }

func (n *nullSource) LastError() error {
	return &goapi.SourceDownError{Op: "stream", Err: errSourceDown}
}

func init() { core.Register(New()) }
