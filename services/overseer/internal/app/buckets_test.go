package app_test

import (
	"altune/overseer/internal/app"
	"slices"
	"testing"
)

func TestTheOverseerBuildsExactlyItsNineBuckets(t *testing.T) {
	var ids []string
	for _, b := range app.Buckets() {
		ids = append(ids, b.Meta().ID)
	}
	slices.Sort(ids)

	want := []string{"backendperf", "cost", "domainquality", "heartbeat", "liveactivity", "logs", "reliability", "security", "usage"}
	if !slices.Equal(ids, want) {
		t.Fatalf("app.Buckets() = %v, want exactly %v", ids, want)
	}
}
