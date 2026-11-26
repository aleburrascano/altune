package apierror_test

import (
	"altune/go-api/internal/shared/apierror"
	"testing"
)

type catalogEntry struct {
	name string
	kind apierror.Kind
}

func TestCatalog_ListsEveryCodeTheAPICanAnswerWith(t *testing.T) {
	want := []catalogEntry{
		{"internal", apierror.KindInternal},
		{"request.invalid_id", apierror.KindInvalid},
		{"request.invalid_body", apierror.KindInvalid},
		{"request.too_large", apierror.KindTooLarge},
		{"request.timeout", apierror.KindTimeout},
		{"auth.throttled", apierror.KindRateLimited},
		{"auth.unavailable", apierror.KindUnavailable},
		{"auth.token_invalid", apierror.KindUnauthenticated},
		{"auth.token_missing", apierror.KindUnauthenticated},
		{"auth.token_malformed", apierror.KindUnauthenticated},
		{"auth.token_signature_invalid", apierror.KindUnauthenticated},
		{"auth.token_expired", apierror.KindUnauthenticated},
		{"auth.token_revoked", apierror.KindUnauthenticated},
		{"auth.token_claim_missing_exp", apierror.KindUnauthenticated},
		{"auth.token_claim_invalid_iss", apierror.KindUnauthenticated},
		{"auth.token_claim_invalid_aud", apierror.KindUnauthenticated},
		{"auth.token_claim_invalid_sub", apierror.KindUnauthenticated},
		{"auth.token_claim_invalid_iat", apierror.KindUnauthenticated},
		{"auth_failure.invalid_body", apierror.KindInvalid},
		{"eventstream.streaming_unsupported", apierror.KindInternal},
		{"eventstream.too_many_streams", apierror.KindRateLimited},
		{"observe.principal_required", apierror.KindForbidden},
		{"observe.discography_quality_timeout", apierror.KindTimeout},
		{"observe.event_feed_unavailable", apierror.KindUnavailable},
		{"observe.stream_subscriber_limit", apierror.KindRateLimited},
		{"observe.streaming_unsupported", apierror.KindInternal},
		{"observe.stream_unavailable", apierror.KindUnavailable},
		{"catalog.track_not_found", apierror.KindNotFound},
		{"catalog.playlist_not_found", apierror.KindNotFound},
		{"catalog.audio_not_available", apierror.KindNotFound},
		{"catalog.audio_temporarily_unavailable", apierror.KindUnavailable},
		{"catalog.temporarily_unavailable", apierror.KindUnavailable},
		{"catalog.audio_orphaned", apierror.KindInternal},
		{"catalog.track_already_in_playlist", apierror.KindConflict},
		{"catalog.track_ids_required", apierror.KindInvalid},
		{"catalog.batch_too_large", apierror.KindInvalid},
		{"catalog.invalid_track_id", apierror.KindInvalid},
		{"catalog.featured_artist_key_required", apierror.KindInvalid},
		{"catalog.invalid_deezer_id", apierror.KindInvalid},
		{"catalog.playlist_full", apierror.KindInvalid},
		{"catalog.too_many_playlists", apierror.KindInvalid},
		{"catalog.library_full", apierror.KindInvalid},
		{"catalog.backfill_cooling_down", apierror.KindRateLimited},
		{"catalog.backfill_in_progress", apierror.KindConflict},
		{"catalog.audio_rate_limited", apierror.KindRateLimited},
		{"catalog.write_rate_limited", apierror.KindRateLimited},
		{"catalog.validation_error", apierror.KindInvalid},
		{"discovery.rate_limited", apierror.KindRateLimited},
		{"discovery.invalid_favorite", apierror.KindInvalid},
		{"discovery.favorites_full", apierror.KindConflict},
		{"discovery.invalid_event", apierror.KindInvalid},
		{"discovery.invalid_body", apierror.KindInvalid},
		{"discovery.invalid_event_type", apierror.KindInvalid},
		{"discovery.invalid_kind", apierror.KindInvalid},
		{"discovery.invalid_param", apierror.KindInvalid},
		{"discovery.invalid_provider", apierror.KindInvalid},
		{"discovery.q_required", apierror.KindInvalid},
		{"discovery.all_providers_failed", apierror.KindUnavailable},
		{"discovery.content_unserved", apierror.KindNotFound},
		{"discovery.provider_timeout", apierror.KindTimeout},
		{"discovery.provider_rate_limited", apierror.KindUnavailable},
		{"discovery.provider_circuit_open", apierror.KindUnavailable},
		{"enrichment_unavailable", apierror.KindUnavailable},
		{"discovery.provider_error", apierror.KindUpstreamFailed},
		{"playback.rate_limited", apierror.KindRateLimited},
		{"playback.unavailable", apierror.KindUnavailable},
		{"playback.stale_queue_write", apierror.KindConflict},
		{"playback.queue_position_mismatch", apierror.KindConflict},
		{"playback.position_ms_negative", apierror.KindInvalid},
		{"playback.queue_too_long", apierror.KindInvalid},
		{"playback.string_too_long", apierror.KindInvalid},
		{"playback.string_contains_nul", apierror.KindInvalid},
		{"playback.current_idx_out_of_range", apierror.KindInvalid},
		{"playback.current_track_id_missing", apierror.KindInvalid},
		{"playback.unknown_repeat_mode", apierror.KindInvalid},
		{"playback.unknown_source_kind", apierror.KindInvalid},
		{"playback.unrecognized_source_id", apierror.KindInvalid},
		{"playback.malformed_track_id", apierror.KindInvalid},
		{"playback.natural_order_mismatch", apierror.KindInvalid},
		{"acquisition.retry_not_failed", apierror.KindConflict},
		{"acquisition.cooldown_active", apierror.KindRateLimited},
		{"acquisition.reacquire_not_ready", apierror.KindConflict},
		{"acquisition.job_in_flight", apierror.KindConflict},
		{"acquisition.shutting_down", apierror.KindUnavailable},
		{"acquisition.paused", apierror.KindUnavailable},
	}

	got := apierror.Catalog()

	if len(got) != len(want) {
		t.Fatalf("catalog has %d codes, want %d: a code was added or removed without updating this list", len(got), len(want))
	}
	for i, w := range want {
		if got[i].String() != w.name || got[i].Kind() != w.kind {
			t.Errorf("catalog[%d] = %s (kind %s), want %s (kind %s)", i, got[i], got[i].Kind(), w.name, w.kind)
		}
		if got[i].Title() == "" {
			t.Errorf("catalog[%d] %s has no title", i, got[i])
		}
	}
}

func TestCatalog_NamesAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range apierror.Catalog() {
		if seen[c.String()] {
			t.Errorf("code %s is in the catalog twice", c)
		}
		seen[c.String()] = true
	}
}

func TestCatalog_ReturnsACopyCallersCannotEdit(t *testing.T) {
	first := apierror.Catalog()
	first[0] = apierror.CodeAuthThrottled

	if got := apierror.Catalog()[0]; got != apierror.CodeInternal {
		t.Errorf("catalog[0] = %s after a caller edited its copy, want internal", got)
	}
}
