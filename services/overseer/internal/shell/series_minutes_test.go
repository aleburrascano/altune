package shell_test

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/history"
	"altune/overseer/internal/shell"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

type minuteSeries struct {
	fakeSeries
	minutes map[string][]history.Minute
}

func (m *minuteSeries) Minutes(_, series string, from, to time.Time) ([]history.Minute, error) {
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
		minutes: map[string][]history.Minute{"latency_ms": {{At: at, Min: 10, Max: 90, Avg: 40}}},
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
