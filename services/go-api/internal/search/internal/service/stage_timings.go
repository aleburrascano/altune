package service

import (
	"altune/go-api/internal/shared/telemetry"
	"context"
	"time"
)

type stageTimings struct {
	fanOut    time.Duration
	rank      time.Duration
	artwork   time.Duration
	favorites time.Duration
	related   time.Duration
}

func (s *SearchService) timeStage(ctx context.Context, name string, into *time.Duration) (context.Context, func()) {
	stageCtx, stage := telemetry.StartStage(ctx, s.now, "search."+name)
	return stageCtx, func() { *into = stage.End(nil) }
}

func (t stageTimings) logAttrs(total time.Duration) []any {
	return []any{
		"fanout_ms", t.fanOut.Milliseconds(),
		"rank_ms", t.rank.Milliseconds(),
		"artwork_ms", t.artwork.Milliseconds(),
		"favorites_ms", t.favorites.Milliseconds(),
		"related_ms", t.related.Milliseconds(),
		"total_ms", total.Milliseconds(),
	}
}

type searchTiming struct {
	stages  stageTimings
	total   time.Duration
	cached  bool
	partial bool
}

func (t searchTiming) addTo(payload map[string]any) {
	attrs := t.stages.logAttrs(t.total)
	for i := 0; i+1 < len(attrs); i += 2 {
		payload[attrs[i].(string)] = attrs[i+1]
	}
	payload["cached"] = t.cached
	payload["partial"] = t.partial
}
