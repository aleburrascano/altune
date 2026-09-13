package service

import (
	"context"
	"log/slog"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared"
)

const (
	telemetryTopN     = 10
	pipelineVersionV2 = "v2"
	emitTimeout       = 3 * time.Second
)

// shownSignaturesCap bounds the per-search proof-of-display list so a huge
// slate cannot bloat the event row.
const shownSignaturesCap = 200

func (s *Service) emitSearchEvent(parentCtx context.Context, userId shared.UserId, searchId, queryNorm string, shown []domain.SearchResult, shownSigs []string, explored bool) {
	if s.eventStore == nil || userId.IsSystem() {
		return
	}

	payload := map[string]any{
		"result_count":     len(shown),
		"zero_result":      len(shown) == 0,
		"tail_noise_top5":  TailNoiseInTopK(shown, 5),
		"pipeline_version": pipelineVersionV2,
		"shown_signatures": shownSigs,
	}
	if explored {
		payload["exploration"] = true
		payload["exploration_rate"] = s.explorationRate
	}
	if top := buildShownTop(shown); len(top) > 0 {
		payload["top"] = top
	}

	s.launchBackground(parentCtx, "telemetry.emit", func(ctx context.Context) {
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
	})
}

// shownSignatures lists, deduplicated and capped, the result_signature of
// every result a search response can surface to the client: the whole ranked
// slate (later pages are served from it but never re-emit search_performed)
// and the related groups. SatisfactionSignals trusts only these (#573).
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

// signatureOf mirrors the handler DTO: a stamped signature wins, otherwise it
// is derived, so the recorded value equals what the client receives.
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
