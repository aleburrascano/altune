package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
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
	bg         *backgroundRunner
}

func newSearchTelemetry(eventStore ports.EventStore, activity ports.ActivityFeed, bg *backgroundRunner) *SearchTelemetry {
	if activity == nil {
		activity = noopActivityFeed{}
	}
	return &SearchTelemetry{eventStore: eventStore, activity: activity, bg: bg}
}

func (t *SearchTelemetry) emit(parentCtx context.Context, userId shared.UserId, searchId, queryNorm string, shown []domain.SearchResult, shownSigs []string, explored bool, explorationRate float64) {
	if t.eventStore == nil {
		return
	}
	if err := shared.GuardNotSystem(userId); err != nil {
		return
	}

	payload := map[string]any{
		"result_count":                   len(shown),
		domain.PayloadKeyZeroResult:      len(shown) == 0,
		domain.PayloadKeyTailNoiseTop5:   TailNoiseInTopK(shown, 5),
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

	t.bg.launch(parentCtx, "telemetry.emit", func(ctx context.Context) {
		emitCtx, cancel := context.WithTimeout(ctx, emitTimeout)
		defer cancel()

		event := domain.InteractionEvent{
			OccurredAt: time.Now().UTC(),
			UserId:     userId,
			Type:       domain.EventTypeSearchPerformed,
			QueryNorm:  queryNorm,
			SearchId:   searchId,
			Payload:    payload,
		}
		if err := t.eventStore.Append(emitCtx, event); err != nil {
			slog.WarnContext(emitCtx, "search.v2.telemetry_emit_failed",
				"search_id", searchId,
				"user_id", userId.String(),
				"error", err)
			return
		}
		t.activity.EmitActivity(domain.EventTypeSearchPerformed.String())
	})
}

func shownSignatures(slate []domain.SearchResult, related []domain.RelatedGroup) []string {
	sigs := make([]string, 0, len(slate))
	seen := make(map[string]bool, len(slate))
	add := func(r domain.SearchResult) {
		sig := signatureOf(r)
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

func signatureOf(r domain.SearchResult) string {
	if r.Signature != "" {
		return r.Signature
	}
	return domain.ResultSignature(r)
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
