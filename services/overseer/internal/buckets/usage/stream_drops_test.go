package usage

import "testing"

type droppingSource struct {
	*fakeSource
	streamDrops int
}

func (d droppingSource) Dropped() int { return d.streamDrops }

func TestSnapshotDroppedReportsStreamDrops(t *testing.T) {
	const streamDrops = 9
	b := newBucket(droppingSource{fakeSource: newFakeSource(1), streamDrops: streamDrops})

	if got := snapData(t, b.Snapshot()).Dropped; got != streamDrops {
		t.Fatalf("snapshot dropped = %d, want %d stream drops", got, streamDrops)
	}
}
