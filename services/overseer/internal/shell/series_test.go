package shell_test

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/history"
	"altune/overseer/internal/shell"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

var dataRoutes = []string{"/api/buckets", "/api/buckets/reliability/series", "/api/stream"}

type fakeSeries struct {
	points  map[string][]core.Point
	names   []string
	failure error
	from    time.Time
	to      time.Time
}

func (f *fakeSeries) Names(string) ([]string, error) { return f.names, f.failure }

func (f *fakeSeries) Query(_, series string, from, to time.Time) ([]core.Point, error) {
	f.from, f.to = from, to
	return f.points[series], nil
}

func reliabilityRegistry() fixedRegistry {
	return fixedRegistry{buckets: []core.Bucket{stubBucket{id: "reliability", state: core.StateLive}}}
}

type seriesBody struct {
	Bucket string `json:"bucket"`
	Range  string `json:"range"`
	Series map[string][]struct {
		At string  `json:"at"`
		V  float64 `json:"v"`
	} `json:"series"`
}

func getSeries(t *testing.T, srv http.Handler, path string) (*httptest.ResponseRecorder, seriesBody) {
	t.Helper()
	rec := do(srv, withOwner(httptest.NewRequest(http.MethodGet, path, nil)))
	var body seriesBody
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode %s: %v (%s)", path, err, rec.Body.String())
		}
	}
	return rec, body
}

func TestEveryDataRouteRejectsAMissingBearer(t *testing.T) {
	srv := newServer(reliabilityRegistry())
	for _, path := range dataRoutes {
		if rec := do(srv, httptest.NewRequest(http.MethodGet, path, nil)); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without bearer = %d, want 401", path, rec.Code)
		}
	}
}

func TestEveryDataRouteForbidsANonOwner(t *testing.T) {
	srv := newServer(reliabilityRegistry())
	for _, path := range dataRoutes {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+nonOwnerToken)
		if rec := do(srv, req); rec.Code != http.StatusForbidden {
			t.Errorf("%s with non-owner token = %d, want 403", path, rec.Code)
		}
	}
}

func TestGuardedHealthDataRejectsMissingBearerAndNonOwner(t *testing.T) {
	t.Skip("lands in [Task]: overseer exposes credential and cycle health to the owner (#2359)")
}

func TestSeriesReturnsEveryNamedSeriesForTheWindow(t *testing.T) {
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	source := &fakeSeries{
		names: []string{"latency_ms", "up"},
		points: map[string][]core.Point{
			"up":         {{At: at, Value: 1}},
			"latency_ms": {{At: at, Value: 38.5}},
		},
	}
	srv := newServer(reliabilityRegistry(), shell.WithSeries(source))

	rec, body := getSeries(t, srv, "/api/buckets/reliability/series?range=24h")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if body.Bucket != "reliability" || body.Range != "24h" {
		t.Fatalf("envelope = %s/%s, want reliability/24h", body.Bucket, body.Range)
	}
	if got := body.Series["up"]; len(got) != 1 || got[0].V != 1 || got[0].At != "2026-09-01T12:00:00Z" {
		t.Fatalf("up series = %+v, want one point {2026-09-01T12:00:00Z 1}", got)
	}
	if got := body.Series["latency_ms"]; len(got) != 1 || got[0].V != 38.5 {
		t.Fatalf("latency_ms series = %+v, want one point of 38.5", got)
	}
	if window := source.to.Sub(source.from); window != 24*time.Hour {
		t.Fatalf("queried window = %v, want 24h", window)
	}
}

func TestSeriesRangeDefaultsToOneHour(t *testing.T) {
	source := &fakeSeries{names: []string{"up"}}
	srv := newServer(reliabilityRegistry(), shell.WithSeries(source))

	rec, body := getSeries(t, srv, "/api/buckets/reliability/series")

	if rec.Code != http.StatusOK || body.Range != "1h" {
		t.Fatalf("status %d range %q, want 200 1h", rec.Code, body.Range)
	}
	if window := source.to.Sub(source.from); window != time.Hour {
		t.Fatalf("queried window = %v, want 1h", window)
	}
	if got, present := body.Series["up"]; !present || got == nil {
		t.Fatalf("an empty series must serialize as [], got %s", rec.Body.String())
	}
}

func TestSeriesRejectsAnUnknownRange(t *testing.T) {
	srv := newServer(reliabilityRegistry(), shell.WithSeries(&fakeSeries{}))
	for _, query := range []string{"range=2h", "range=1H", "range=7d;drop", "range=30d", "range=", "range=1h&range=7d", "range=%zz"} {
		rec, _ := getSeries(t, srv, "/api/buckets/reliability/series?"+query)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("?%s = %d, want 400", query, rec.Code)
		}
	}
}

func TestSeriesUnknownBucketIsNotFound(t *testing.T) {
	srv := newServer(reliabilityRegistry(), shell.WithSeries(&fakeSeries{names: []string{"up"}}))
	rec, _ := getSeries(t, srv, "/api/buckets/nope/series")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown bucket = %d, want 404", rec.Code)
	}
}

func TestSeriesWithNoHistoryIsAnEmptySeriesMap(t *testing.T) {
	srv := newServer(reliabilityRegistry())
	rec, _ := getSeries(t, srv, "/api/buckets/reliability/series")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != `{"bucket":"reliability","range":"1h","series":{}}` {
		t.Fatalf("body = %s, want an empty series map", got)
	}
}

func TestSeriesReadFailureIsUnavailableAndLogged(t *testing.T) {
	srv := newServer(reliabilityRegistry(), shell.WithSeries(&fakeSeries{failure: errors.New("disk I/O error")}))
	logs := captureSlog(t)

	rec, _ := getSeries(t, srv, "/api/buckets/reliability/series")

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if len(logRecordsNamed(logs, "overseer.shell.series_read_failed")) != 1 {
		t.Fatalf("read failure not logged: %s", logs.String())
	}
}

type minuteSeries struct {
	fakeSeries
	minutes map[string][]core.Minute
}

func (m *minuteSeries) Minutes(_, series string, from, to time.Time) ([]core.Minute, error) {
	m.from, m.to = from, to
	return m.minutes[series], nil
}

type wirePoint struct {
	At  string   `json:"at"`
	V   float64  `json:"v"`
	Min *float64 `json:"min"`
	Max *float64 `json:"max"`
}

func getWireSeries(t *testing.T, srv http.Handler, path string) map[string][]wirePoint {
	t.Helper()
	rec := do(srv, withOwner(httptest.NewRequest(http.MethodGet, path, nil)))
	if rec.Code != http.StatusOK {
		t.Fatalf("%s = %d, want 200 (%s)", path, rec.Code, rec.Body.String())
	}
	var body struct {
		Series map[string][]wirePoint `json:"series"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return body.Series
}

func minuteFixture() *minuteSeries {
	at := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	return &minuteSeries{
		fakeSeries: fakeSeries{
			names:  []string{"latency_ms"},
			points: map[string][]core.Point{"latency_ms": {{At: at, Value: 38.5}}},
		},
		minutes: map[string][]core.Minute{"latency_ms": {{At: at, Min: 10, Max: 90, Avg: 40}}},
	}
}

func TestSevenDaySeriesServesMinuteAveragesWithMinAndMax(t *testing.T) {
	source := minuteFixture()
	srv := newServer(reliabilityRegistry(), shell.WithSeries(source))

	series := getWireSeries(t, srv, "/api/buckets/reliability/series?range=7d")

	got := series["latency_ms"]
	if len(got) != 1 || got[0].V != 40 || got[0].Min == nil || *got[0].Min != 10 || got[0].Max == nil || *got[0].Max != 90 {
		t.Fatalf("7d latency_ms = %+v, want one minute {v 40 min 10 max 90}", got)
	}
	if window := source.to.Sub(source.from); window != 7*24*time.Hour {
		t.Fatalf("queried window = %v, want 7d", window)
	}
}

func TestDaySeriesServesRawPointsWithoutMinOrMax(t *testing.T) {
	srv := newServer(reliabilityRegistry(), shell.WithSeries(minuteFixture()))

	series := getWireSeries(t, srv, "/api/buckets/reliability/series?range=24h")

	got := series["latency_ms"]
	if len(got) != 1 || got[0].V != 38.5 || got[0].Min != nil || got[0].Max != nil {
		t.Fatalf("24h latency_ms = %+v, want the raw point with no min or max", got)
	}
}

func TestSevenDaySeriesFromAReaderWithoutMinutesServesItsPoints(t *testing.T) {
	source := minuteFixture()
	srv := newServer(reliabilityRegistry(), shell.WithSeries(&source.fakeSeries))

	series := getWireSeries(t, srv, "/api/buckets/reliability/series?range=7d")

	if got := series["latency_ms"]; len(got) != 1 || got[0].V != 38.5 || got[0].Min != nil {
		t.Fatalf("7d latency_ms from a points-only reader = %+v, want its one point", got)
	}
}

func TestSevenDaySeriesFromTheHistoryStoreCarriesMinAndMax(t *testing.T) {
	store := history.Open(filepath.Join(t.TempDir(), "history.db"))
	t.Cleanup(func() { _ = store.Close() })
	minute := time.Now().UTC().Add(-time.Hour).Truncate(time.Minute)
	for i, value := range []float64{20, 60, 40} {
		store.Record("reliability", "latency_ms", core.Point{At: minute.Add(time.Duration(i) * 10 * time.Second), Value: value})
	}
	srv := newServer(reliabilityRegistry(), shell.WithSeries(store))

	series := getWireSeries(t, srv, "/api/buckets/reliability/series?range=7d")

	got := series["latency_ms"]
	if len(got) != 1 || got[0].V != 40 || got[0].Min == nil || *got[0].Min != 20 || got[0].Max == nil || *got[0].Max != 60 {
		t.Fatalf("7d latency_ms from the store = %+v, want one minute {v 40 min 20 max 60}", got)
	}
}
