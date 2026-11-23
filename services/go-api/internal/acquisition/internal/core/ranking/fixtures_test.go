package ranking_test

import (
	"altune/go-api/internal/acquisition"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func song(title, artist string, length float64) acquisition.RankingTrack {
	return acquisition.RankingTrack{Title: title, Artist: artist, ExpectedLength: length}
}

func rank(track acquisition.RankingTrack, candidates ...acquisition.AudioCandidate) ([]acquisition.AudioCandidate, []acquisition.RankingRefusal) {
	return acquisition.Rank(context.Background(), track, candidates)
}

func best(track acquisition.RankingTrack, candidates ...acquisition.AudioCandidate) *acquisition.AudioCandidate {
	ranked, _ := rank(track, candidates...)
	if len(ranked) == 0 {
		return nil
	}
	return &ranked[0]
}

func urls(candidates []acquisition.AudioCandidate) []string {
	out := make([]string, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, c.URL)
	}
	return out
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func evaluations(t *testing.T, track acquisition.RankingTrack, candidates ...acquisition.AudioCandidate) []map[string]any {
	t.Helper()
	buf := captureLog(t)
	rank(track, candidates...)
	var records []map[string]any
	lines := bufio.NewScanner(buf)
	for lines.Scan() {
		var record map[string]any
		if err := json.Unmarshal(lines.Bytes(), &record); err != nil {
			t.Fatalf("unparseable log line %q: %v", lines.Text(), err)
		}
		if record["msg"] == "candidate_evaluated" {
			records = append(records, record)
		}
	}
	return records
}

func evaluation(t *testing.T, track acquisition.RankingTrack, c acquisition.AudioCandidate) map[string]any {
	t.Helper()
	records := evaluations(t, track, c)
	if len(records) != 1 {
		t.Fatalf("logged %d candidate_evaluated records for %q, want 1", len(records), c.Title)
	}
	return records[0]
}

func number(t *testing.T, record map[string]any, key string) float64 {
	t.Helper()
	value, ok := record[key].(float64)
	if !ok {
		t.Fatalf("%s = %v, want a number in %v", key, record[key], record)
	}
	return value
}

func joinLabels(labels []string) string {
	return strings.Join(labels, "|")
}

func assertLabels(t *testing.T, name string, got, want []string) {
	t.Helper()
	if joinLabels(got) != joinLabels(want) {
		t.Errorf("%s = %v, want %v", name, got, want)
	}
}
