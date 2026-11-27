package candidateranking_test

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/download/internal/core/candidateranking"
	"altune/go-api/internal/shared/logging"
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func song(title, artist string, length float64) candidateranking.Track {
	return candidateranking.Track{Title: title, Artist: artist, ExpectedLength: length}
}

func rank(track candidateranking.Track, candidates ...download.AudioCandidate) ([]download.AudioCandidate, []candidateranking.Refusal) {
	return candidateranking.Rank(context.Background(), track, candidates)
}

func best(track candidateranking.Track, candidates ...download.AudioCandidate) *download.AudioCandidate {
	ranked, _ := rank(track, candidates...)
	if len(ranked) == 0 {
		return nil
	}
	return &ranked[0]
}

func urls(candidates []download.AudioCandidate) []string {
	out := make([]string, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, c.URL)
	}
	return out
}

func evaluations(t *testing.T, track candidateranking.Track, candidates ...download.AudioCandidate) []map[string]any {
	t.Helper()
	buf := logging.CaptureJSON(t, slog.LevelInfo)
	rank(track, candidates...)
	var records []map[string]any
	lines := bufio.NewScanner(strings.NewReader(buf.String()))
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

func evaluation(t *testing.T, track candidateranking.Track, c download.AudioCandidate) map[string]any {
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
