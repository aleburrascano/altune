package logs

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type fakeSource struct {
	ch     chan goapi.LogRecord
	status goapi.Status
}

func newFakeSource(status goapi.Status, buffer int) *fakeSource {
	return &fakeSource{ch: make(chan goapi.LogRecord, buffer), status: status}
}

func (f *fakeSource) Run(ctx context.Context) error   { <-ctx.Done(); return ctx.Err() }
func (f *fakeSource) Records() <-chan goapi.LogRecord { return f.ch }
func (f *fakeSource) Status() goapi.Status            { return f.status }

func drive(t *testing.T, b *Bucket) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	signals, _ := b.Collect(ctx)
	b.Store(signals)
}

func snapData(t *testing.T, snap core.Snapshot) Data {
	t.Helper()
	var d Data
	if err := json.Unmarshal(snap.Data, &d); err != nil {
		t.Fatalf("unmarshal data: %v (%s)", err, snap.Data)
	}
	return d
}

type pumpSource struct {
	ch      chan goapi.LogRecord
	stopped chan struct{}
}

func newPumpSource() *pumpSource {
	return &pumpSource{ch: make(chan goapi.LogRecord, 64), stopped: make(chan struct{})}
}

func (p *pumpSource) Run(ctx context.Context) error {
	defer close(p.stopped)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			select {
			case p.ch <- goapi.LogRecord{Level: "INFO", Message: "tick"}:
			default:
			}
		}
	}
}

func (p *pumpSource) Records() <-chan goapi.LogRecord { return p.ch }
func (p *pumpSource) Status() goapi.Status            { return goapi.StatusUp }

func TestStartKeepsPumpFeedingPastFirstTick(t *testing.T) {
	src := newPumpSource()
	b := newBucket(src, "")
	appCtx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	b.Start(appCtx)

	firstCtx, firstCancel := context.WithCancel(context.Background())
	first, _ := b.Collect(firstCtx)
	b.Store(first)
	firstCancel()

	seen := len(first)
	deadline := time.After(2 * time.Second)
	for seen < len(first)+logCapacity {
		select {
		case <-deadline:
			t.Fatalf("pump delivered %d records then froze after the first tick's ctx was cancelled — the #1812 regression", seen)
		default:
		}
		s, _ := b.Collect(context.Background())
		b.Store(s)
		seen += len(s)
	}

	cancel()
	select {
	case <-src.stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("pump goroutine did not exit after app ctx cancel — leaked past shutdown")
	}
}

func TestBucketRegisters(t *testing.T) {
	found := false
	for _, bk := range core.Default.Buckets() {
		if bk.Meta().ID == "logs" {
			found = true
		}
	}
	if !found {
		t.Fatal("logs bucket did not self-register into the default registry")
	}
}

func TestCollectStoreSnapshotRoundTrip(t *testing.T) {
	src := newFakeSource(goapi.StatusUp, 8)
	src.ch <- goapi.LogRecord{Time: time.Now().UTC(), Level: "INFO", Message: "queue resumed", Fields: map[string]string{"queue": "q1"}}
	src.ch <- goapi.LogRecord{Time: time.Now().UTC(), Level: "ERROR", Message: "boom", Fields: map[string]string{"err": "x"}}
	b := newBucket(src, "")

	drive(t, b)

	snap := b.Snapshot()
	if snap.State != core.StateLive {
		t.Errorf("state = %q, want live", snap.State)
	}
	d := snapData(t, snap)
	if len(d.Records) != 2 {
		t.Fatalf("records = %d, want 2", len(d.Records))
	}
	if d.Records[0].Message != "queue resumed" || d.Records[0].Fields["queue"] != "q1" {
		t.Fatalf("first record not carried: %+v", d.Records[0])
	}
	if d.Records[1].Level != "ERROR" || d.Records[1].Message != "boom" {
		t.Fatalf("second record not carried: %+v", d.Records[1])
	}
}

func TestRingStaysCapped(t *testing.T) {
	src := newFakeSource(goapi.StatusUp, 0)
	b := newBucket(src, "")
	over := logCapacity + 50
	signals := make([]core.Signal, 0, over)
	for i := 0; i < over; i++ {
		signals = append(signals, toSignal(goapi.LogRecord{Level: "INFO", Message: "x"}))
	}
	b.Store(signals)
	if got := b.records.Len(); got != logCapacity {
		t.Fatalf("ring retained %d records, want cap %d", got, logCapacity)
	}
	if got := b.records.Cap(); got != logCapacity {
		t.Fatalf("ring cap = %d, want %d", got, logCapacity)
	}
}

func TestSnapshotSurfacesDroppedCount(t *testing.T) {
	src := newFakeSource(goapi.StatusUp, 0)
	b := newBucket(src, "")
	const overflow = 25
	signals := make([]core.Signal, 0, logCapacity+overflow)
	for i := 0; i < logCapacity+overflow; i++ {
		signals = append(signals, toSignal(goapi.LogRecord{Level: "INFO", Message: "x"}))
	}
	b.Store(signals)

	if got := snapData(t, b.Snapshot()).Dropped; got != overflow {
		t.Fatalf("snapshot dropped = %d, want %d (records beyond cap)", got, overflow)
	}
}

func TestLevelFilter(t *testing.T) {
	records := []core.Signal{
		{Kind: "DEBUG", Text: `{"msg":"d"}`},
		{Kind: "INFO", Text: `{"msg":"i"}`},
		{Kind: "WARN", Text: `{"msg":"w"}`},
		{Kind: "ERROR", Text: `{"msg":"e"}`},
	}
	got := filteredRecords(records, "WARN")
	if len(got) != 2 {
		t.Fatalf("≥WARN kept %d records, want 2: %+v", len(got), got)
	}
	for _, r := range got {
		if r.Level == "INFO" || r.Level == "DEBUG" {
			t.Fatalf("≥WARN leaked a %s line", r.Level)
		}
	}
	if all := filteredRecords(records, ""); len(all) != 4 {
		t.Fatalf("empty min kept %d records, want all 4", len(all))
	}
}

func TestSnapshotFiltersByConfiguredLevel(t *testing.T) {
	src := newFakeSource(goapi.StatusUp, 4)
	src.ch <- goapi.LogRecord{Level: "INFO", Message: "info-line"}
	src.ch <- goapi.LogRecord{Level: "ERROR", Message: "error-line"}
	b := newBucket(src, "ERROR")

	drive(t, b)

	d := snapData(t, b.Snapshot())
	if d.MinLevel != "ERROR" {
		t.Errorf("minLevel = %q, want ERROR", d.MinLevel)
	}
	if len(d.Records) != 1 || d.Records[0].Message != "error-line" {
		t.Fatalf("≥ERROR tail = %+v, want only error-line", d.Records)
	}
}

func TestDegradeToSourceDown(t *testing.T) {
	src := newFakeSource(goapi.StatusDown, 0)
	b := newBucket(src, "")

	b.Store([]core.Signal{toSignal(goapi.LogRecord{Level: "INFO", Message: "last known"})})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := b.Collect(ctx); !errors.Is(err, errSourceDown) {
		t.Fatalf("Collect with down source returned %v, want errSourceDown", err)
	}
	snap := b.Snapshot()
	if snap.State != core.StateSourceDown {
		t.Errorf("state = %q, want source_down", snap.State)
	}
	d := snapData(t, snap)
	if len(d.Records) != 1 || d.Records[0].Message != "last known" {
		t.Fatalf("stale snapshot dropped the last-known tail: %+v", d.Records)
	}
}

func TestSeverityCriticalWhenTheTailHoldsAnError(t *testing.T) {
	src := newFakeSource(goapi.StatusUp, 4)
	src.ch <- goapi.LogRecord{Time: time.Now().UTC(), Level: "INFO", Message: "started"}
	src.ch <- goapi.LogRecord{Time: time.Now().UTC(), Level: "ERROR", Message: "boom"}
	b := newBucket(src, "")
	drive(t, b)

	snap := b.Snapshot()

	if snap.Severity != core.SeverityCritical {
		t.Errorf("severity = %q, want critical", snap.Severity)
	}
	if snap.State != core.StateLive {
		t.Errorf("state = %q, want live — an error line is not a stale source", snap.State)
	}
	if snap.Headline != "1 errors · 0 warnings · 2 lines" {
		t.Errorf("headline = %q, want the per-level counts", snap.Headline)
	}
}

func TestSeverityWarnsWhenTheTailHoldsOnlyWarnings(t *testing.T) {
	src := newFakeSource(goapi.StatusUp, 4)
	src.ch <- goapi.LogRecord{Time: time.Now().UTC(), Level: "WARN", Message: "retrying"}
	b := newBucket(src, "")
	drive(t, b)

	if got := b.Snapshot().Severity; got != core.SeverityWarn {
		t.Errorf("severity = %q, want warn", got)
	}
}

func TestSeverityOKWhenTheTailIsClean(t *testing.T) {
	src := newFakeSource(goapi.StatusUp, 4)
	src.ch <- goapi.LogRecord{Time: time.Now().UTC(), Level: "INFO", Message: "started"}
	b := newBucket(src, "")
	drive(t, b)

	snap := b.Snapshot()

	if snap.Severity != core.SeverityOK {
		t.Errorf("severity = %q, want ok", snap.Severity)
	}
	if snap.Headline != "0 errors · 0 warnings · 1 lines" {
		t.Errorf("headline = %q, want the per-level counts", snap.Headline)
	}
}

func TestToSignalRedactsDenylistedKeys(t *testing.T) {
	for _, key := range []string{"token", "authorization", "email", "password", "secret"} {
		rec := decodeRecord(toSignal(goapi.LogRecord{
			Level:   "INFO",
			Message: "auth attempt",
			Fields:  map[string]string{key: "s3cr3t-value", "queue": "q1"},
		}))
		if rec.Fields[key] != redactedValue {
			t.Errorf("key %q stored as %q, want %q", key, rec.Fields[key], redactedValue)
		}
		if rec.Fields["queue"] != "q1" {
			t.Errorf("key %q redaction disturbed a benign attr: %q", key, rec.Fields["queue"])
		}
		if rec.Message != "auth attempt" {
			t.Errorf("key %q redaction disturbed the message: %q", key, rec.Message)
		}
	}
}

func TestToSignalRedactsKeyVariants(t *testing.T) {
	for _, key := range []string{"Authorization", "ACCESS TOKEN", "user.email", "api-key", "X-Auth-Token", "Api_Key", "Set-Cookie"} {
		rec := decodeRecord(toSignal(goapi.LogRecord{
			Level:  "INFO",
			Fields: map[string]string{key: "leak"},
		}))
		if rec.Fields[key] != redactedValue {
			t.Errorf("variant %q stored as %q, want redacted", key, rec.Fields[key])
		}
	}
}

func TestSnapshotCarriesRawFields(t *testing.T) {
	src := newFakeSource(goapi.StatusUp, 1)
	src.ch <- goapi.LogRecord{
		Level:   "INFO",
		Message: `<script>alert('msg')</script>`,
		Fields:  map[string]string{`<k>`: `<v>"&`},
	}
	b := newBucket(src, "")

	drive(t, b)

	d := snapData(t, b.Snapshot())
	if len(d.Records) != 1 {
		t.Fatalf("records = %d, want 1", len(d.Records))
	}
	rec := d.Records[0]
	if rec.Message != `<script>alert('msg')</script>` {
		t.Fatalf("message not carried verbatim: %q", rec.Message)
	}
	if rec.Fields[`<k>`] != `<v>"&` {
		t.Fatalf("field key/value not carried verbatim: %+v", rec.Fields)
	}
}
