package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"context"
	"log/slog"
	"time"
)

// DiscographyTelemetry is the structural-quality collaborator for the
// artist-content path. It emits the server-only discography_observed event —
// the per-release cross-provider disagreement already computed by MergeReleases
// — off the response path. It mirrors SearchTelemetry (telemetry.go): the event
// schema lives here so it can change without touching the fan-out orchestrator.
//
// The signal is structural, so the payload carries no user identity: only the
// resolved artist ref, the release count, the single-provider count, the
// single-provider-without-a-shared-id count (the real contamination suspects —
// see buildDiscographyPayload), and the per-provider release counts. The
// observation time is the event's occurred_at column, the single source of truth
// the aggregate reads — it is deliberately not repeated in the payload.
type DiscographyTelemetry struct {
	eventStore ports.EventStore
	bg         *backgroundRunner
}

func newDiscographyTelemetry(eventStore ports.EventStore) *DiscographyTelemetry {
	return &DiscographyTelemetry{eventStore: eventStore, bg: &backgroundRunner{}}
}

// emit records one discography_observed event best-effort and off the hot path.
// The payload is summarized synchronously from merged (a fresh map that never
// aliases the caller's slice, so the async Append shares no mutable state and
// -race stays clean), and both the summarization and the Append are guarded so a
// panic or error is recovered and dropped: the artist response is never failed or
// slowed by this emit. A nil telemetry or nil store is a no-op.
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
			// The system identity keeps the row off any real account: this is a
			// structural signal, not a user's behavior.
			UserId:  shared.SystemUserId(),
			Type:    domain.EventTypeDiscographyObserved,
			Payload: payload,
		}
		if err := t.eventStore.Append(emitCtx, event); err != nil {
			slog.WarnContext(emitCtx, "discography.telemetry.emit_failed", "error", err)
		}
	})
}

// safePayload builds the event payload under a recover so a panic while
// summarizing the merged set (e.g. a corrupt provider set) is contained and the
// emit is simply dropped rather than propagating into the request goroutine.
func (t *DiscographyTelemetry) safePayload(ctx context.Context, artistRef string, merged []MergedRelease) (payload map[string]any, ok bool) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.WarnContext(ctx, "discography.telemetry.build_panicked", "error", rec)
			payload, ok = nil, false
		}
	}()
	return buildDiscographyPayload(artistRef, merged), true
}

// buildDiscographyPayload summarizes the merged releases into the pinned
// discography_observed shape: {artist_ref, releases, single_provider,
// single_provider_no_id, provider_counts}. single_provider counts releases
// carried by exactly one provider (len(Providers)==1); single_provider_no_id
// counts those of them that also lack a shared id — the real contamination
// suspects, since a lone-provider release still backed by a strong or verified id
// (HasStrongID/IDVerified, already computed at the merge) is provably the same
// recording, not a suspect. provider_counts is, per provider, how many releases it
// supplied. No user id. No timestamp: the observation time is the event's
// occurred_at column, which the aggregate reads.
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

// idBacked reports whether a merged release carries a shared id anchor: a strong
// id (ISRC/MBID/UPC) or a verified one. A single-provider release that is idBacked
// is not a contamination suspect — the id, not the provider headcount, is the
// anchor. Both fields are computed at the merge (release_merge.go); this reads
// them, it never recomputes the id verdict.
func idBacked(m MergedRelease) bool {
	return m.HasStrongID || m.IDVerified
}
