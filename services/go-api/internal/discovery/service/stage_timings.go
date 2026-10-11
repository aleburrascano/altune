package service

import "time"

type stageTimings struct {
	fanOut    time.Duration
	identity  time.Duration
	rank      time.Duration
	artwork   time.Duration
	favorites time.Duration
	related   time.Duration
}

func timeStage(into *time.Duration) func() {
	start := time.Now()
	return func() { *into = time.Since(start) }
}

func (t stageTimings) logAttrs(total time.Duration) []any {
	return []any{
		"fanout_ms", t.fanOut.Milliseconds(),
		"identity_ms", t.identity.Milliseconds(),
		"rank_ms", t.rank.Milliseconds(),
		"artwork_ms", t.artwork.Milliseconds(),
		"favorites_ms", t.favorites.Milliseconds(),
		"related_ms", t.related.Milliseconds(),
		"total_ms", total.Milliseconds(),
	}
}
