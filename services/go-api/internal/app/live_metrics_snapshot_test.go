package app

import (
	"altune/go-api/internal/shared/reqmetrics"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	providermetrics "altune/go-api/internal/discovery/adapters/providermetrics"
	playbackmetrics "altune/go-api/internal/playback/adapters/metrics"
)

type statusTransport struct{ status int }

func (s statusTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: s.status, Body: http.NoBody, Header: make(http.Header), Request: req}, nil
}

func TestLiveMetricsSnapshot_CarriesProviderBreakerAndLatency(t *testing.T) {
	read := func() (out struct {
		Providers map[string]struct {
			OK    int64 `json:"ok"`
			Quota int64 `json:"quota"`
		} `json:"providers"`
		Playback struct {
			Open       bool  `json:"now_playing_enrichment_breaker_open"`
			Rejections int64 `json:"now_playing_enrichment_breaker_rejections_total"`
		} `json:"playback"`
		Latency struct {
			Routes map[string]struct {
				Count  uint64            `json:"count"`
				Status map[string]uint64 `json:"status"`
			} `json:"routes"`
		} `json:"latency"`
	}, raw []byte,
	) {
		raw, err := json.Marshal(liveMetricsSnapshot())
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		return out, raw
	}
	before, _ := read()

	const secret = "supersecretquery"
	for url, status := range map[string]int{
		"https://api.deezer.com/search?q=" + secret:     http.StatusOK,
		"https://api.spotify.com/v1/search?q=" + secret: http.StatusTooManyRequests,
	} {
		resp, err := providermetrics.NewCountingTransport(statusTransport{status}).RoundTrip(httptest.NewRequest(http.MethodGet, url, nil))
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
	}
	pb := playbackmetrics.NewExpvarPlaybackMetrics()
	pb.EnrichmentBreakerOpened()
	pb.EnrichmentBreakerRejected()
	defer pb.EnrichmentBreakerClosed()
	const route = "/v1/live-snapshot-probe/{id}"
	reqmetrics.Observe(route, 4*time.Millisecond, http.StatusOK)
	reqmetrics.Observe(route, time.Millisecond, http.StatusNotFound)
	reqmetrics.Observe(route, time.Millisecond, http.StatusBadGateway)

	got, raw := read()
	if got.Providers["deezer"].OK != before.Providers["deezer"].OK+1 {
		t.Errorf("providers.deezer.ok = %d, want %d", got.Providers["deezer"].OK, before.Providers["deezer"].OK+1)
	}
	if got.Providers["spotify"].Quota != before.Providers["spotify"].Quota+1 {
		t.Errorf("providers.spotify.quota = %d, want %d", got.Providers["spotify"].Quota, before.Providers["spotify"].Quota+1)
	}
	if !got.Playback.Open || got.Playback.Rejections != before.Playback.Rejections+1 {
		t.Errorf("breaker open=%v rejections=%d, want open and %d", got.Playback.Open, got.Playback.Rejections, before.Playback.Rejections+1)
	}
	rl := got.Latency.Routes[route]
	if rl.Count != 3 || rl.Status["2xx"] != 1 || rl.Status["4xx"] != 1 || rl.Status["5xx"] != 1 {
		t.Errorf("latency.routes[%q] = %+v, want count 3 and one each of 2xx/4xx/5xx", route, rl)
	}
	if strings.Contains(string(raw), secret) {
		t.Errorf("snapshot leaks query text %q", secret)
	}
}
