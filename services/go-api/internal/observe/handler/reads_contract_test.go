package handler

import (
	acqPorts "altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/observe/evalmeter"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

type overseerLiveMetrics struct {
	Latency overseerLatencyMetrics `json:"latency"`
}

type overseerLatencyMetrics struct {
	Routes map[string]overseerRouteLatency `json:"routes"`
}

type overseerRouteLatency struct {
	Count   uint64                   `json:"count"`
	SumMs   uint64                   `json:"sum_ms"`
	Buckets []overseerLatencyBucket  `json:"buckets"`
	Status  overseerRouteStatusClass `json:"status"`
}

type overseerLatencyBucket struct {
	LeMs  string `json:"le_ms"`
	Count uint64 `json:"count"`
}

type overseerRouteStatusClass struct {
	Count2xx uint64 `json:"2xx"`
	Count4xx uint64 `json:"4xx"`
	Count5xx uint64 `json:"5xx"`
}

type overseerEvalStatus struct {
	Enabled  bool                `json:"enabled"`
	Paused   bool                `json:"paused"`
	State    string              `json:"state"`
	Score    *float64            `json:"score,omitempty"`
	Baseline *float64            `json:"baseline,omitempty"`
	LastRun  *time.Time          `json:"last_run,omitempty"`
	Error    string              `json:"error,omitempty"`
	Queries  []overseerEvalQuery `json:"queries,omitempty"`
}

type overseerEvalQuery struct {
	Query    string `json:"query"`
	Expect   string `json:"expect"`
	Passed   bool   `json:"passed"`
	Position int    `json:"position"`
}

type overseerAcquisitionStatus struct {
	InFlight      int    `json:"in_flight"`
	Succeeded     uint64 `json:"succeeded"`
	Failed        uint64 `json:"failed"`
	Rejected      uint64 `json:"rejected"`
	QueueDepth    int    `json:"queue_depth"`
	QueueCapacity int    `json:"queue_capacity"`
}

type overseerDiscographyQuality struct {
	WindowDays   int                       `json:"window_days"`
	GroupBy      string                    `json:"group_by"`
	Cases        []overseerDiscographyCase `json:"cases"`
	SuspectRate  float64                   `json:"suspect_rate"`
	LastSampleAt time.Time                 `json:"last_sample_at"`
}

type overseerDiscographyCase struct {
	Artist             string         `json:"artist"`
	ArtistRef          string         `json:"artist_ref"`
	Releases           int            `json:"releases"`
	SingleProvider     int            `json:"single_provider"`
	SingleProviderNoID int            `json:"single_provider_no_id"`
	ProviderCounts     map[string]int `json:"provider_counts"`
	LastSeen           time.Time      `json:"last_seen"`
}

func assertBodyCarriesKeys(t *testing.T, body []byte, mirror reflect.Type) {
	t.Helper()
	object := decodeObject(t, body)
	for _, key := range jsonKeys(mirror) {
		if _, present := object[key]; !present {
			t.Errorf("body %s lacks %q, which Overseer's %s decodes", body, key, mirror.Name())
		}
	}
}

func TestContract_MetricsLiveBodyCarriesEveryKeyOverseerDecodes(t *testing.T) {
	deps := Deps{LiveMetrics: func() LiveMetrics {
		return LiveMetrics{"latency": map[string]any{"routes": map[string]any{
			"/v1/tracks/{trackId}": map[string]any{
				"count":   1,
				"sum_ms":  1,
				"buckets": []any{map[string]any{"le_ms": "10", "count": 1}},
				"status":  map[string]any{"2xx": 1, "4xx": 0, "5xx": 0},
			},
		}}}
	}}
	rec := serveRead(t, deps, "/metrics/live")
	assertBodyCarriesKeys(t, rec.Body.Bytes(), reflect.TypeOf(overseerLiveMetrics{}))

	body := decodeObject(t, rec.Body.Bytes())
	latency := decodeObject(t, body["latency"])
	routes := decodeObject(t, latency["routes"])
	assertBodyCarriesKeys(t, routes["/v1/tracks/{trackId}"], reflect.TypeOf(overseerRouteLatency{}))
}

func TestContract_EvalBodyCarriesTheRequiredKeys(t *testing.T) {
	rec := serveRead(t, Deps{Eval: evalmeter.New(true, 0, nil)}, "/eval")
	body := decodeObject(t, rec.Body.Bytes())
	for _, key := range []string{"enabled", "paused", "state"} {
		if _, present := body[key]; !present {
			t.Errorf("body %s lacks %q, which Overseer's overseerEvalStatus decodes", rec.Body.Bytes(), key)
		}
	}
}

func TestContract_EvalBodyDecodesIntoOverseerMirror(t *testing.T) {
	rec := serveRead(t, Deps{Eval: evalmeter.New(true, 0, nil)}, "/eval")

	var got overseerEvalStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode into Overseer's mirror: %v", err)
	}
	if !got.Enabled {
		t.Errorf("enabled = false, want true")
	}
}

func TestContract_AcquisitionBodyCarriesEveryKeyOverseerDecodes(t *testing.T) {
	reader := fakeAcquisitionReader{status: acqPorts.AcquisitionStatus{
		InFlight: 1, Succeeded: 2, Failed: 3, Rejected: 4, QueueDepth: 5, QueueCapacity: 6,
	}}
	rec := serveRead(t, Deps{Acquisition: reader}, "/acquisition")
	assertBodyCarriesKeys(t, rec.Body.Bytes(), reflect.TypeOf(overseerAcquisitionStatus{}))

	var got overseerAcquisitionStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode into Overseer's mirror: %v", err)
	}
	if got.InFlight != 1 || got.QueueCapacity != 6 {
		t.Errorf("status = %+v, want the reader's counters", got)
	}
}

func TestContract_QualityBodyCarriesEveryKeyOverseerDecodes(t *testing.T) {
	seen := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	reader := &fakeDiscographyReader{
		cases: []ports.DiscographyCase{{
			ArtistRef:      "spotify:artist",
			Releases:       4,
			SingleProvider: 1,
			ProviderCounts: map[string]int{"spotify": 4},
			LastSeen:       seen,
		}},
		suspect: ports.DiscographySuspectRate{Rate: 0.5, LastSample: seen},
	}
	rec := serveRead(t, Deps{Discography: reader}, "/quality/discography")
	assertBodyCarriesKeys(t, rec.Body.Bytes(), reflect.TypeOf(overseerDiscographyQuality{}))

	body := decodeObject(t, rec.Body.Bytes())
	var cases []json.RawMessage
	if err := json.Unmarshal(body["cases"], &cases); err != nil {
		t.Fatalf("decode cases: %v", err)
	}
	if len(cases) != 1 {
		t.Fatalf("cases = %d, want 1", len(cases))
	}
	assertBodyCarriesKeys(t, cases[0], reflect.TypeOf(overseerDiscographyCase{}))
}
