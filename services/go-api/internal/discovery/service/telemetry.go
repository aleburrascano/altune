package service

import (
	"context"
	"log/slog"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared"
)

const (
	// telemetryTopN caps how many shown results the envelope records.
	telemetryTopN = 10
	// pipelineVersionV2 stamps every rebuilt-pipeline event so ML training data
	// stays attributable per pipeline across the cutover.
	pipelineVersionV2 = "v2"
	// emitTimeout bounds a single telemetry append.
	emitTimeout = 3 * time.Second
)

// emitSearchEvent appends a search_performed telemetry event, asynchronously and
// best-effort: it never blocks the request, outlives request cancellation (via
// WithoutCancel + its own timeout), recovers from panics, and logs — never
// surfaces — failures. The envelope matches the legacy emitter, stamped v2.
func (s *Service) emitSearchEvent(parentCtx context.Context, userId shared.UserId, searchId, queryNorm string, shown []domain.SearchResult) {
	if s.eventStore == nil {
		return
	}

	payload := map[string]any{
		"result_count":     len(shown),
		"zero_result":      len(shown) == 0,
		"pipeline_version": pipelineVersionV2,
	}
	if top := buildShownTop(shown); len(top) > 0 {
		payload["top"] = top
	}

	ctx := context.WithoutCancel(parentCtx)
	s.bgWg.Add(1)
	go func() {
		defer s.bgWg.Done()
		defer func() {
			if r := recover(); r != nil {
				slog.Warn("search.v2.telemetry_emit_panic", "error", r)
			}
		}()

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
		if err := s.eventStore.Append(emitCtx, event); err != nil {
			slog.WarnContext(emitCtx, "search.v2.telemetry_emit_failed", "error", err)
		}
	}()
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
