package liveactivity

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
	src := droppingSource{fakeSource: newFakeSource(eventCapacity + ringOverflow), streamDrops: streamDrops}
	b := newBucket(src)
	for range eventCapacity + ringOverflow {
		src.push(goapi.Event{Type: "track.played"})
	}
	collectStore(t, b)

	if got := snapData(t, b.Snapshot()).Dropped; got != ringOverflow+streamDrops {
		t.Fatalf("snapshot dropped = %d, want %d ring evictions + %d stream drops", got, ringOverflow, streamDrops)
	}
}
