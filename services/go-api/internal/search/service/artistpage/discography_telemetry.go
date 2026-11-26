package artistpage

import (
	"altune/go-api/internal/search/domain"
	"altune/go-api/internal/search/internal/core/albumgrouping"
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/ids"
	"context"
	"log/slog"
	"time"
)

const emitTimeout = 3 * time.Second

type DiscographyTelemetry struct {
	eventStore ports.EventStore
	bg         *background.Tracker
}

func newDiscographyTelemetry(eventStore ports.EventStore, bg *background.Tracker) *DiscographyTelemetry {
	return &DiscographyTelemetry{eventStore: eventStore, bg: bg}
}

func (t *DiscographyTelemetry) emit(parentCtx context.Context, artistRef string, merged []albumgrouping.MergedRelease) {
	if t == nil || t.eventStore == nil {
		return
	}
	payload, ok := t.safePayload(parentCtx, artistRef, merged)
	if !ok {
		return
	}
	occurredAt := time.Now().UTC()
	t.bg.LaunchWithin(parentCtx, "discography.telemetry.emit", emitTimeout, func(ctx context.Context) {
		t.append(ctx, occurredAt, payload)
	})
}

func (t *DiscographyTelemetry) append(ctx context.Context, occurredAt time.Time, payload map[string]any) {
	event := domain.InteractionEvent{
		OccurredAt: occurredAt,
		UserId:     ids.SystemUserId(),
		Type:       domain.EventTypeDiscographyObserved,
		Payload:    payload,
	}
	if err := t.eventStore.Append(ctx, event); err != nil {
		slog.WarnContext(ctx, "discography.telemetry.emit_failed", "error", err)
	}
}

func (t *DiscographyTelemetry) safePayload(ctx context.Context, artistRef string, merged []albumgrouping.MergedRelease) (payload map[string]any, ok bool) {
	defer background.RecoverContext(ctx, "discography.telemetry.build_panicked")
	return buildDiscographyPayload(artistRef, merged), true
}

func buildDiscographyPayload(artistRef string, merged []albumgrouping.MergedRelease) map[string]any {
	providerCounts, singleProvider, singleProviderNoID := countProviders(merged)
	return map[string]any{
		domain.PayloadKeyArtistRef:          artistRef,
		domain.PayloadKeyReleases:           len(merged),
		domain.PayloadKeySingleProvider:     singleProvider,
		domain.PayloadKeySingleProviderNoId: singleProviderNoID,
		domain.PayloadKeyProviderCounts:     providerCounts,
	}
}

func countProviders(merged []albumgrouping.MergedRelease) (providerCounts map[string]int, singleProvider, singleProviderNoID int) {
	providerCounts = make(map[string]int)
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
	return providerCounts, singleProvider, singleProviderNoID
}

func idBacked(m albumgrouping.MergedRelease) bool {
	return m.HasStrongID || m.IDVerified
}
