package domain_test

import (
	"altune/go-api/internal/shared/domain"
	"testing"
)

type catalogEntry struct {
	name string
	kind domain.Kind
}

func TestCatalog_ListsEveryCodeTheAPICanAnswerWith(t *testing.T) {
	want := []catalogEntry{
		{"internal", domain.KindInternal},
		{"request.invalid_id", domain.KindInvalid},
		{"request.invalid_body", domain.KindInvalid},
		{"request.too_large", domain.KindTooLarge},
		{"request.timeout", domain.KindTimeout},
		{"auth.throttled", domain.KindRateLimited},
		{"auth.unavailable", domain.KindUnavailable},
		{"auth.token_invalid", domain.KindUnauthenticated},
		{"auth.token_missing", domain.KindUnauthenticated},
		{"auth.token_malformed", domain.KindUnauthenticated},
		{"auth.token_signature_invalid", domain.KindUnauthenticated},
		{"auth.token_expired", domain.KindUnauthenticated},
		{"auth.token_revoked", domain.KindUnauthenticated},
		{"auth.token_claim_missing_exp", domain.KindUnauthenticated},
		{"auth.token_claim_invalid_iss", domain.KindUnauthenticated},
		{"auth.token_claim_invalid_aud", domain.KindUnauthenticated},
		{"auth.token_claim_invalid_sub", domain.KindUnauthenticated},
		{"auth.token_claim_invalid_iat", domain.KindUnauthenticated},
		{"auth_failure.invalid_body", domain.KindInvalid},
		{"eventstream.streaming_unsupported", domain.KindInternal},
		{"eventstream.too_many_streams", domain.KindRateLimited},
		{"observe.principal_required", domain.KindForbidden},
		{"observe.discography_quality_timeout", domain.KindTimeout},
		{"observe.event_feed_unavailable", domain.KindUnavailable},
		{"observe.stream_subscriber_limit", domain.KindRateLimited},
		{"observe.streaming_unsupported", domain.KindInternal},
		{"observe.stream_unavailable", domain.KindUnavailable},
		{"catalog.track_not_found", domain.KindNotFound},
		{"catalog.playlist_not_found", domain.KindNotFound},
		{"catalog.audio_not_available", domain.KindNotFound},
		{"catalog.audio_temporarily_unavailable", domain.KindUnavailable},
		{"catalog.temporarily_unavailable", domain.KindUnavailable},
		{"catalog.audio_orphaned", domain.KindInternal},
		{"catalog.track_already_in_playlist", domain.KindConflict},
		{"catalog.track_ids_required", domain.KindInvalid},
		{"catalog.batch_too_large", domain.KindInvalid},
		{"catalog.invalid_track_id", domain.KindInvalid},
		{"catalog.featured_artist_key_required", domain.KindInvalid},
		{"catalog.invalid_deezer_id", domain.KindInvalid},
		{"catalog.playlist_full", domain.KindInvalid},
		{"catalog.too_many_playlists", domain.KindInvalid},
		{"catalog.library_full", domain.KindInvalid},
		{"catalog.backfill_cooling_down", domain.KindRateLimited},
		{"catalog.backfill_in_progress", domain.KindConflict},
		{"catalog.audio_rate_limited", domain.KindRateLimited},
		{"catalog.write_rate_limited", domain.KindRateLimited},
		{"catalog.validation_error", domain.KindInvalid},
		{"discovery.rate_limited", domain.KindRateLimited},
		{"discovery.invalid_favorite", domain.KindInvalid},
		{"discovery.favorites_full", domain.KindConflict},
		{"discovery.invalid_event", domain.KindInvalid},
		{"discovery.invalid_body", domain.KindInvalid},
		{"discovery.invalid_event_type", domain.KindInvalid},
		{"discovery.invalid_kind", domain.KindInvalid},
		{"discovery.invalid_param", domain.KindInvalid},
		{"discovery.invalid_provider", domain.KindInvalid},
		{"discovery.q_required", domain.KindInvalid},
		{"discovery.all_providers_failed", domain.KindUnavailable},
		{"discovery.content_unserved", domain.KindNotFound},
		{"discovery.provider_timeout", domain.KindTimeout},
		{"discovery.provider_rate_limited", domain.KindUnavailable},
		{"discovery.provider_circuit_open", domain.KindUnavailable},
		{"discovery.provider_error", domain.KindUpstreamFailed},
		{"playback.rate_limited", domain.KindRateLimited},
		{"playback.unavailable", domain.KindUnavailable},
		{"playback.stale_queue_write", domain.KindConflict},
		{"playback.queue_position_mismatch", domain.KindConflict},
		{"playback.position_ms_negative", domain.KindInvalid},
		{"playback.queue_too_long", domain.KindInvalid},
		{"playback.string_too_long", domain.KindInvalid},
		{"playback.string_contains_nul", domain.KindInvalid},
		{"playback.current_idx_out_of_range", domain.KindInvalid},
		{"playback.current_track_id_missing", domain.KindInvalid},
		{"playback.unknown_repeat_mode", domain.KindInvalid},
		{"playback.unknown_source_kind", domain.KindInvalid},
		{"playback.unrecognized_source_id", domain.KindInvalid},
		{"playback.malformed_track_id", domain.KindInvalid},
		{"playback.natural_order_mismatch", domain.KindInvalid},
		{"acquisition.retry_not_failed", domain.KindConflict},
		{"acquisition.cooldown_active", domain.KindRateLimited},
		{"acquisition.reacquire_not_ready", domain.KindConflict},
		{"acquisition.job_in_flight", domain.KindConflict},
		{"acquisition.shutting_down", domain.KindUnavailable},
		{"acquisition.paused", domain.KindUnavailable}}

	got := domain.Catalog()

	if len(got) != len(want) {
		t.Fatalf("catalog has %d codes, want %d: a code was added or removed without updating this list", len(got), len(want))
	}
	for i, w := range want {
		if got[i].String() != w.name || got[i].Kind() != w.kind {
			t.Errorf("catalog[%d] = %s (kind %d), want %s (kind %d)", i, got[i], got[i].Kind(), w.name, w.kind)
		}
		if got[i].Title() == "" {
			t.Errorf("catalog[%d] %s has no title", i, got[i])
		}
	}
}

func TestCatalog_NamesAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range domain.Catalog() {
		if seen[c.String()] {
			t.Errorf("code %s is in the catalog twice", c)
		}
		seen[c.String()] = true
	}
}

func TestCatalog_ReturnsACopyCallersCannotEdit(t *testing.T) {
	first := domain.Catalog()
	first[0] = domain.CodeAuthThrottled

	if got := domain.Catalog()[0]; got != domain.CodeInternal {
		t.Errorf("catalog[0] = %s after a caller edited its copy, want internal", got)
	}
}
