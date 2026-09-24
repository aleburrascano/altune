package shell

import (
	"altune/overseer/internal/core"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"
)

const defaultSeriesRange = "1h"

var seriesWindows = map[string]time.Duration{
	"1h":  time.Hour,
	"24h": 24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
}

type SeriesReader interface {
	Names(bucket string) ([]string, error)
	Query(bucket, series string, from, to time.Time) ([]core.Point, error)
}

func WithSeries(reader SeriesReader) Option {
	return func(h *Handler) {
		if reader != nil {
			h.series = reader
		}
	}
}

type noSeries struct{}

func (noSeries) Names(string) ([]string, error) { return nil, nil }

func (noSeries) Query(string, string, time.Time, time.Time) ([]core.Point, error) {
	return nil, nil
}

type seriesPoint struct {
	At time.Time `json:"at"`
	V  float64   `json:"v"`
}

type seriesResponse struct {
	Bucket string                   `json:"bucket"`
	Range  string                   `json:"range"`
	Series map[string][]seriesPoint `json:"series"`
}

func (h *Handler) handleSeries(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !h.isRegistered(id) {
		http.Error(w, "unknown bucket", http.StatusNotFound)
		return
	}
	rangeName, window, ok := parseSeriesRange(r.URL.RawQuery)
	if !ok {
		http.Error(w, "range must be one of 1h, 24h, 7d", http.StatusBadRequest)
		return
	}
	to := time.Now().UTC()
	series, err := h.readSeries(id, to.Add(-window), to)
	if err != nil {
		slog.ErrorContext(r.Context(), "overseer.shell.series_read_failed", "bucket", id, "range", rangeName, "error", err)
		http.Error(w, "history read failed", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, seriesResponse{Bucket: id, Range: rangeName, Series: series})
}

func parseSeriesRange(rawQuery string) (string, time.Duration, bool) {
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return "", 0, false
	}
	values := query["range"]
	switch len(values) {
	case 0:
		return defaultSeriesRange, seriesWindows[defaultSeriesRange], true
	case 1:
		window, ok := seriesWindows[values[0]]
		return values[0], window, ok
	default:
		return "", 0, false
	}
}

func (h *Handler) isRegistered(id string) bool {
	for _, b := range h.registry.Buckets() {
		if b.Meta().ID == id {
			return true
		}
	}
	return false
}

func (h *Handler) readSeries(bucket string, from, to time.Time) (map[string][]seriesPoint, error) {
	names, err := h.series.Names(bucket)
	if err != nil {
		return nil, err
	}
	series := make(map[string][]seriesPoint, len(names))
	for _, name := range names {
		points, err := h.series.Query(bucket, name, from, to)
		if err != nil {
			return nil, err
		}
		wire := make([]seriesPoint, 0, len(points))
		for _, p := range points {
			wire = append(wire, seriesPoint{At: p.At.UTC(), V: p.Value})
		}
		series[name] = wire
	}
	return series, nil
}
