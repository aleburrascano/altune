package domainquality

import (
	"altune/overseer/internal/goapi"
	"context"
	"testing"
	"time"
)

// blockedEvalReader drives one read (eval) that blocks until its own context is
// cancelled, and answers acquisition/discography from their own per-call
// context — each returning an error if THAT context is already done, rather
// than sharing the caller's outer deadline. It proves the three reads run on
// independent contexts: under sequential reads sharing one outer deadline,
// acquisition and discography would only be called once eval's block released
// the shared deadline, by which time it would already be expired.
type blockedEvalReader struct {
	acq   goapi.AcquisitionStatus
	disco goapi.DiscographyQuality
}

func (blockedEvalReader) AdminEval(ctx context.Context) (goapi.EvalStatus, error) {
	<-ctx.Done()
	return goapi.EvalStatus{}, ctx.Err()
}

func (r blockedEvalReader) AdminAcquisition(ctx context.Context) (goapi.AcquisitionStatus, error) {
	if err := ctx.Err(); err != nil {
		return goapi.AcquisitionStatus{}, err
	}
	return r.acq, nil
}

func (r blockedEvalReader) AdminDiscographyQuality(ctx context.Context) (goapi.DiscographyQuality, error) {
	if err := ctx.Err(); err != nil {
		return goapi.DiscographyQuality{}, err
	}
	return r.disco, nil
}

type panicEvalReader struct {
	acq   goapi.AcquisitionStatus
	disco goapi.DiscographyQuality
}

func (panicEvalReader) AdminEval(context.Context) (goapi.EvalStatus, error) {
	panic("boom: eval reader misbehaved")
}

func (r panicEvalReader) AdminAcquisition(context.Context) (goapi.AcquisitionStatus, error) {
	return r.acq, nil
}

func (r panicEvalReader) AdminDiscographyQuality(context.Context) (goapi.DiscographyQuality, error) {
	return r.disco, nil
}

func TestParallelReadPanicDoesNotCrashProcess(t *testing.T) {
	reader := panicEvalReader{
		acq: goapi.AcquisitionStatus{Succeeded: 19, Failed: 1, InFlight: 2, QueueDepth: 4, QueueCapacity: 64},
		disco: goapi.DiscographyQuality{
			WindowDays: 30, GroupBy: "artist",
			Cases: []goapi.DiscographyCase{{Artist: "Radiohead", Releases: 42, SingleProvider: 9}},
		},
	}
	b := newBucket(reader)

	if _, err := b.Collect(context.Background()); err != nil {
		t.Fatalf("Collect errored though acquisition and discography were live: %v", err)
	}

	d := snapData(t, b.Snapshot())
	if !d.EvalStale || d.Eval != nil {
		t.Fatalf("panicking eval side not flagged stale: %+v", d)
	}
	if d.AcqStale || d.Acquisition == nil || d.Acquisition.Succeeded != 19 {
		t.Fatalf("acquisition not recorded fresh despite the panicking eval read: %+v", d)
	}
	if d.DiscoStale || d.Discography == nil {
		t.Fatalf("discography not recorded fresh despite the panicking eval read: %+v", d)
	}
}

// TestParallelReadsIsolateASlowOne is the ticket's Done proof: eval blocks past
// its own deadline while acquisition and discography still record fresh data in
// the same Collect cycle, because each read runs on its own context rather than
// waiting its turn behind a read ahead of it.
func TestParallelReadsIsolateASlowOne(t *testing.T) {
	reader := blockedEvalReader{
		acq: goapi.AcquisitionStatus{Succeeded: 19, Failed: 1, InFlight: 2, QueueDepth: 4, QueueCapacity: 64},
		disco: goapi.DiscographyQuality{
			WindowDays: 30, GroupBy: "artist",
			Cases: []goapi.DiscographyCase{{Artist: "Radiohead", Releases: 42, SingleProvider: 9}},
		},
	}
	b := newBucket(reader)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := b.Collect(ctx); err != nil {
		t.Fatalf("Collect errored though acquisition and discography were live: %v", err)
	}

	d := snapData(t, b.Snapshot())
	if !d.EvalStale || d.Eval != nil {
		t.Fatalf("blocked eval side not flagged stale: %+v", d)
	}
	if d.AcqStale || d.Acquisition == nil || d.Acquisition.Succeeded != 19 {
		t.Fatalf("acquisition not recorded fresh despite the blocked eval read: %+v", d)
	}
	if d.DiscoStale || d.Discography == nil {
		t.Fatalf("discography not recorded fresh despite the blocked eval read: %+v", d)
	}
}
