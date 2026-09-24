package logs

import (
	"altune/overseer/internal/goapi"
	"testing"
)

type droppingSource struct {
	*fakeSource
	streamDrops int
}

func (d droppingSource) Dropped() int { return d.streamDrops }

func TestSnapshotDroppedCountsStreamDropsWithRingEvictions(t *testing.T) {
	const ringOverflow, streamDrops = 5, 7
	src := droppingSource{fakeSource: newFakeSource(goapi.StatusUp, logCapacity+ringOverflow), streamDrops: streamDrops}
	b := newBucket(src, "")
	for range logCapacity + ringOverflow {
		src.ch <- goapi.LogRecord{Level: "INFO", Message: "x"}
	}
	drive(t, b)

	if got := snapData(t, b.Snapshot()).Dropped; got != ringOverflow+streamDrops {
		t.Fatalf("snapshot dropped = %d, want %d ring evictions + %d stream drops", got, ringOverflow, streamDrops)
	}
}
