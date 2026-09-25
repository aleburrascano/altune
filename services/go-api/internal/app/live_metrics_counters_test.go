package app

import (
	"encoding/json"
	"testing"

	authmetrics "altune/go-api/internal/auth/adapters/metrics"
	catalogmetrics "altune/go-api/internal/catalog/adapters/metrics"
	feedbackmetrics "altune/go-api/internal/feedback/adapters/metrics"
	playbackmetrics "altune/go-api/internal/playback/adapters/metrics"
)

type liveCounterWire struct {
	Auth struct {
		TokenRejections     int64            `json:"token_rejections_total"`
		ByReason            map[string]int64 `json:"token_rejections_by_reason_total"`
		VerifierUnavailable int64            `json:"verifier_unavailable_total"`
		JWKSFetchFailures   int64            `json:"jwks_fetch_failures_total"`
	} `json:"auth"`
	Catalog struct {
		PresignFailures int64 `json:"presign_failures_total"`
	} `json:"catalog"`
	Feedback struct {
		TrackerCreateFailures int64            `json:"tracker_create_failures_total"`
		ByCause               map[string]int64 `json:"tracker_create_failures_by_cause_total"`
	} `json:"feedback"`
	Playback struct {
		EnrichmentFailures       int64 `json:"now_playing_enrichment_failures_total"`
		CorruptStoredState       int64 `json:"corrupt_stored_state_total"`
		QueueStateOpTimeouts     int64 `json:"queue_state_op_timeouts_total"`
		NowPlayingLookupTimeouts int64 `json:"now_playing_lookup_timeouts_total"`
	} `json:"playback"`
}

func readLiveCounterWire(t *testing.T) liveCounterWire {
	t.Helper()
	raw, err := json.Marshal(liveMetricsSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	var out liveCounterWire
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestLiveMetricsSnapshot_CarriesAuthCatalogFeedbackAndPlaybackFailureCounters(t *testing.T) {
	before := readLiveCounterWire(t)

	authmetrics.NewExpvarAuthMetrics().TokenRejected("signature_invalid")
	authmetrics.NewExpvarAuthMetrics().VerifierUnavailable()
	authmetrics.NewExpvarAuthMetrics().JWKSFetchFailed()
	catalogmetrics.NewExpvarAudioStoreMetrics().PresignFailed()
	feedbackmetrics.NewExpvarFeedbackMetrics().TrackerCreateFailed("tracker_unavailable")
	pb := playbackmetrics.NewExpvarPlaybackMetrics()
	pb.EnrichmentFailed()
	pb.CorruptStoredState()
	pb.QueueStateOpTimedOut()
	pb.NowPlayingLookupTimedOut()

	got := readLiveCounterWire(t)
	for _, c := range []struct {
		key       string
		got, want int64
	}{
		{"auth.token_rejections_total", got.Auth.TokenRejections, before.Auth.TokenRejections + 1},
		{"auth.token_rejections_by_reason_total[signature_invalid]", got.Auth.ByReason["signature_invalid"], before.Auth.ByReason["signature_invalid"] + 1},
		{"auth.verifier_unavailable_total", got.Auth.VerifierUnavailable, before.Auth.VerifierUnavailable + 1},
		{"auth.jwks_fetch_failures_total", got.Auth.JWKSFetchFailures, before.Auth.JWKSFetchFailures + 1},
		{"catalog.presign_failures_total", got.Catalog.PresignFailures, before.Catalog.PresignFailures + 1},
		{"feedback.tracker_create_failures_total", got.Feedback.TrackerCreateFailures, before.Feedback.TrackerCreateFailures + 1},
		{"feedback.tracker_create_failures_by_cause_total[tracker_unavailable]", got.Feedback.ByCause["tracker_unavailable"], before.Feedback.ByCause["tracker_unavailable"] + 1},
		{"playback.now_playing_enrichment_failures_total", got.Playback.EnrichmentFailures, before.Playback.EnrichmentFailures + 1},
		{"playback.corrupt_stored_state_total", got.Playback.CorruptStoredState, before.Playback.CorruptStoredState + 1},
		{"playback.queue_state_op_timeouts_total", got.Playback.QueueStateOpTimeouts, before.Playback.QueueStateOpTimeouts + 1},
		{"playback.now_playing_lookup_timeouts_total", got.Playback.NowPlayingLookupTimeouts, before.Playback.NowPlayingLookupTimeouts + 1},
	} {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.key, c.got, c.want)
		}
	}
}
