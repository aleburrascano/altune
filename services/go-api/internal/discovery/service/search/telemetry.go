package search

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/discovery/service/interaction"
	"altune/go-api/internal/discovery/service/rank"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/runloop"
	"context"
	"log/slog"
	"time"
)

const (
	telemetryTopN     = 10
	pipelineVersionV2 = "v2"
	emitTimeout       = 3 * time.Second
)

const shownSignaturesCap = 200

type SearchTelemetry struct {
	eventStore ports.EventStore
	activity   ports.ActivityFeed
	bg         *runloop.Tracker
}

func newSearchTelemetry(eventStore ports.EventStore, activity ports.ActivityFeed, bg *runloop.Tracker) *SearchTelemetry {
	if activity == nil {
		activity = interaction.NoopActivityFeed{}
	}
	return &SearchTelemetry{eventStore: eventStore, activity: activity, bg: bg}
}

func (t *SearchTelemetry) emit(parentCtx context.Context, run searchRun, page firstPage, explorationRate float64) {
	if t.eventStore == nil {
		return
	}
	if err := shared.GuardNotSystem(run.userId); err != nil {
		return
	}
	payload := searchPerformedPayload(page.shown, shownSignatures(page.fullSlate, page.related), page.explored, explorationRate)
	page.timing.addTo(payload)
	event := domain.InteractionEvent{
		OccurredAt: run.startedAt,
		UserId:     run.userId,
		Type:       domain.EventTypeSearchPerformed,
		QueryNorm:  run.queryNorm,
		SearchId:   run.searchId,
		Payload:    payload,
	}
	t.bg.Launch(parentCtx, "telemetry.emit", func(ctx context.Context) { t.append(ctx, event) })
}

func searchPerformedPayload(shown []domain.SearchResult, shownSigs []string, explored bool, explorationRate float64) map[string]any {
	payload := map[string]any{
		"result_count":                   len(shown),
		domain.PayloadKeyZeroResult:      len(shown) == 0,
		domain.PayloadKeyTailNoiseTop5:   rank.TailNoiseInTopK(shown, 5),
		"pipeline_version":               pipelineVersionV2,
		domain.PayloadKeyShownSignatures: shownSigs,
	}
	if explored {
		payload["exploration"] = true
		payload["exploration_rate"] = explorationRate
	}
	if top := buildShownTop(shown); len(top) > 0 {
		payload["top"] = top
	}
	return payload
}

func (t *SearchTelemetry) append(ctx context.Context, event domain.InteractionEvent) {
	emitCtx, cancel := context.WithTimeout(ctx, emitTimeout)
	defer cancel()
	if err := t.eventStore.Append(emitCtx, event); err != nil {
		slog.WarnContext(emitCtx, "search.v2.telemetry_emit_failed",
			"search_id", event.SearchId,
			"user_id", event.UserId.String(),
			"error", err)
		return
	}
	t.activity.EmitActivity(domain.EventTypeSearchPerformed.String())
}

func shownSignatures(slate []domain.SearchResult, related []domain.RelatedGroup) []string {
	sigs := make([]string, 0, len(slate))
	seen := make(map[string]bool, len(slate))
	add := func(r domain.SearchResult) {
		sig := domain.SignatureOf(r)
		if seen[sig] || len(sigs) >= shownSignaturesCap {
			return
		}
		seen[sig] = true
		sigs = append(sigs, sig)
	}
	for _, r := range slate {
		add(r)
	}
	for _, g := range related {
		for _, r := range g.Items {
			add(r)
		}
	}
	return sigs
}

func buildShownTop(results []domain.SearchResult) []map[string]any {
	n := len(results)
	if n > telemetryTopN {
		n = telemetryTopN
	}
	top := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		r := results[i]
		providers := make([]string, 0, len(r.Sources))
		for _, src := range r.Sources {
			providers = append(providers, src.Provider.String())
		}
		top = append(top, map[string]any{
			"position": i,
			"kind":     r.Kind.String(),
			"title":    r.Title,
			"subtitle": r.Subtitle,
			"sources":  providers,
		})
	}
	return top
}
