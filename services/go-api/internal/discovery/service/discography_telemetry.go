package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"context"
	"log/slog"
	"time"
)

type DiscographyTelemetry struct {
	eventStore ports.EventStore
	bg         *backgroundRunner
}

func newDiscographyTelemetry(eventStore ports.EventStore) *DiscographyTelemetry {
	return &DiscographyTelemetry{eventStore: eventStore, bg: &backgroundRunner{}}
}

func (t *DiscographyTelemetry) emit(parentCtx context.Context, artistRef string, merged []MergedRelease) {
	if t == nil || t.eventStore == nil {
		return
	}
	payload, ok := t.safePayload(parentCtx, artistRef, merged)
	if !ok {
		return
	}
	occurredAt := time.Now().UTC()
	t.bg.launch(parentCtx, "discography.telemetry.emit", func(ctx context.Context) {
		emitCtx, cancel := context.WithTimeout(ctx, emitTimeout)
		defer cancel()

		event := domain.InteractionEvent{
			OccurredAt: occurredAt,
			UserId:     shared.SystemUserId(),
			Type:       domain.EventTypeDiscographyObserved,
			Payload:    payload,
		}
		if err := t.eventStore.Append(emitCtx, event); err != nil {
			slog.WarnContext(emitCtx, "discography.telemetry.emit_failed", "error", err)
		}
	})
}

func (t *DiscographyTelemetry) safePayload(ctx context.Context, artistRef string, merged []MergedRelease) (payload map[string]any, ok bool) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.WarnContext(ctx, "discography.telemetry.build_panicked", "error", rec)
			payload, ok = nil, false
		}
	}()
	return buildDiscographyPayload(artistRef, merged), true
}

func buildDiscographyPayload(artistRef string, merged []MergedRelease) map[string]any {
	providerCounts := make(map[string]int)
	singleProvider := 0
	singleProviderNoID := 0
	for _, m := range merged {
		if len(m.Providers) == 1 {
			singleProvider++
			if !idBacked(m) {
				singleProviderNoID++
			}
		}
		for p := range m.Providers {
			providerCounts[p.String()]++
		}
	}
	return map[string]any{
		domain.PayloadKeyArtistRef:          artistRef,
		domain.PayloadKeyReleases:           len(merged),
		domain.PayloadKeySingleProvider:     singleProvider,
		domain.PayloadKeySingleProviderNoId: singleProviderNoID,
		domain.PayloadKeyProviderCounts:     providerCounts,
	}
}

func idBacked(m MergedRelease) bool {
	return m.HasStrongID || m.IDVerified
}
