package candidateranking_test

import (
	"altune/go-api/internal/download/internal/core/candidateranking"
	"altune/go-api/internal/download/internal/core/ports"
	"strings"
	"testing"
)

func song(title, artist string, length float64) candidateranking.Track {
	return candidateranking.Track{Title: title, Artist: artist, ExpectedLength: length}
}

func rank(track candidateranking.Track, candidates ...ports.AudioCandidate) ([]ports.AudioCandidate, []candidateranking.Refusal) {
	ranked, refused, _ := candidateranking.Rank(track, candidates)
	return ranked, refused
}

func best(track candidateranking.Track, candidates ...ports.AudioCandidate) *ports.AudioCandidate {
	ranked, _ := rank(track, candidates...)
	if len(ranked) == 0 {
		return nil
	}
	return &ranked[0]
}

func urls(candidates []ports.AudioCandidate) []string {
	out := make([]string, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, c.URL)
	}
	return out
}

func evaluations(t *testing.T, track candidateranking.Track, candidates ...ports.AudioCandidate) []map[string]any {
	t.Helper()
	_, _, evaluated := candidateranking.Rank(track, candidates)
	records := make([]map[string]any, 0, len(evaluated))
	for _, ev := range evaluated {
		records = append(records, map[string]any{
			"identity_score":     ev.IdentityScore,
			"metadata_rank":      ev.MetadataRank,
			"qualifier_distance": float64(ev.QualifierDistance),
			"is_topic":           ev.IsTopic,
			"artist_match":       ev.ArtistMatch,
			"feature_match":      ev.FeatureMatch,
		})
	}
	return records
}

func evaluation(t *testing.T, track candidateranking.Track, c ports.AudioCandidate) map[string]any {
	t.Helper()
	records := evaluations(t, track, c)
	if len(records) != 1 {
		t.Fatalf("evaluated %d candidates for %q, want 1", len(records), c.Title)
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
