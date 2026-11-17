package liveactivity_test

import (
	"altune/overseer/internal/buckets/liveactivity"
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
	b := liveactivity.New(liveactivity.Deps{Source: src})
	for range eventCapacity + ringOverflow {
		src.push(goapi.Event{Type: "track.played"})
	}
	collectStore(t, b)

	if got := snapData(t, b.Snapshot()).Dropped; got != ringOverflow+streamDrops {
		t.Fatalf("snapshot dropped = %d, want %d ring evictions + %d stream drops", got, ringOverflow, streamDrops)
	}
}
