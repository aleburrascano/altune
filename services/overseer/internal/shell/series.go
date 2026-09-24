package shell

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/history"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"
)

const defaultSeriesRange = "1h"

type seriesRange struct {
	window   time.Duration
	byMinute bool
}

var seriesRanges = map[string]seriesRange{
	"1h":  {window: time.Hour},
	"24h": {window: 24 * time.Hour},
	"7d":  {window: 7 * 24 * time.Hour, byMinute: true},
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

type MinuteReader interface {
	Minutes(bucket, series string, from, to time.Time) ([]history.Minute, error)
}

type noSeries struct{}

func (noSeries) Names(string) ([]string, error) { return nil, nil }

func (noSeries) Query(string, string, time.Time, time.Time) ([]core.Point, error) {
	return nil, nil
}

type seriesPoint struct {
	At  time.Time `json:"at"`
	V   float64   `json:"v"`
	Min *float64  `json:"min,omitempty"`
	Max *float64  `json:"max,omitempty"`
}

type seriesResponse struct {
	Bucket string                   `json:"bucket"`
	Range  string                   `json:"range"`
	Series map[string][]seriesPoint `json:"series"`
}

func (h *Handler) handleSeries(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, ok := h.registry.Get(id); !ok {
		http.Error(w, "unknown bucket", http.StatusNotFound)
		return
	}
	rangeName, span, ok := parseSeriesRange(r.URL.RawQuery)
	if !ok {
		http.Error(w, "range must be one of 1h, 24h, 7d", http.StatusBadRequest)
		return
	}
	to := time.Now().UTC()
	series, err := h.readSeries(id, h.pointReader(span), to.Add(-span.window), to)
	if err != nil {
		slog.ErrorContext(r.Context(), "overseer.shell.series_read_failed", "bucket", id, "range", rangeName, "error", err)
		http.Error(w, "history read failed", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, seriesResponse{Bucket: id, Range: rangeName, Series: series})
}

func parseSeriesRange(rawQuery string) (string, seriesRange, bool) {
	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		return "", seriesRange{}, false
	}
	values := query["range"]
	switch len(values) {
	case 0:
		return defaultSeriesRange, seriesRanges[defaultSeriesRange], true
	case 1:
		span, ok := seriesRanges[values[0]]
		return values[0], span, ok
	default:
		return "", seriesRange{}, false
	}
}

type pointReader func(bucket, series string, from, to time.Time) ([]seriesPoint, error)

func (h *Handler) pointReader(span seriesRange) pointReader {
	minutes, readsMinutes := h.series.(MinuteReader)
	if span.byMinute && readsMinutes {
		return minuteAverages(minutes)
	}
	return rawPoints(h.series)
}

func rawPoints(reader SeriesReader) pointReader {
	return func(bucket, series string, from, to time.Time) ([]seriesPoint, error) {
		points, err := reader.Query(bucket, series, from, to)
		if err != nil {
			return nil, err
		}
		wire := make([]seriesPoint, 0, len(points))
		for _, p := range points {
			wire = append(wire, seriesPoint{At: p.At.UTC(), V: p.Value})
		}
		return wire, nil
	}
}

func minuteAverages(reader MinuteReader) pointReader {
	return func(bucket, series string, from, to time.Time) ([]seriesPoint, error) {
		minutes, err := reader.Minutes(bucket, series, from, to)
		if err != nil {
			return nil, err
		}
		wire := make([]seriesPoint, 0, len(minutes))
		for _, m := range minutes {
			wire = append(wire, seriesPoint{At: m.At.UTC(), V: m.Avg, Min: &m.Min, Max: &m.Max})
		}
		return wire, nil
	}
}

func (h *Handler) readSeries(bucket string, read pointReader, from, to time.Time) (map[string][]seriesPoint, error) {
	names, err := h.series.Names(bucket)
	if err != nil {
		return nil, err
	}
	series := make(map[string][]seriesPoint, len(names))
	for _, name := range names {
		points, err := read(bucket, name, from, to)
		if err != nil {
			return nil, err
		}
		series[name] = points
	}
	return series, nil
}
