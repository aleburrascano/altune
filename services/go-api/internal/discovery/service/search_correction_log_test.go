package service

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func loggedEvent(t *testing.T, buf *bytes.Buffer, msg string) string {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if strings.Contains(line, `"msg":"`+msg+`"`) {
			return line
		}
	}
	t.Fatalf("expected a %q event, got:\n%s", msg, buf.String())
	return ""
}

// TestService_Correcting_DoesNotLogRawQueryText guards the same #1097 erasure
// promise as the search lifecycle logs: the auto-correct path fingerprints the
// before/after query text instead of copying it into stdout, so clear-history
// deletion is not silently voided by log retention.
func TestService_Correcting_DoesNotLogRawQueryText(t *testing.T) {
	buf := captureLogs(t)

	const raw = "humbel"
	p := &queryFakeProvider{
		name: domain.ProviderDeezer,
		resultsByQuery: map[string][]domain.SearchResult{
			"humble": {deezerTrack("HUMBLE.", "Kendrick Lamar", 80)},
		},
	}
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker(), WithVocabularyStore(humbleVocab()))
	runSearch(t, svc, raw)
	svc.WaitForBackground()

	event := loggedEvent(t, buf, "search.v2.correcting")
	if strings.Contains(event, raw) {
		t.Fatalf("correcting log leaks raw query text %q:\n%s", raw, event)
	}
	if !strings.Contains(event, `"fp"`) {
		t.Fatalf("expected a search-text fingerprint in place of raw text:\n%s", event)
	}
}

// TestCorrectTokens_DoesNotLogRawTokenText guards the per-token correction
// debug log: the raw token is a per-word slice of the user's query and must be
// fingerprinted, not emitted verbatim under a redactor-blind key.
func TestCorrectTokens_DoesNotLogRawTokenText(t *testing.T) {
	buf := captureLogs(t)

	store := &fakeVocabularyStore{
		findClosestFn: func(token string, _ int) ([]domain.VocabularyEntry, error) {
			if token == "humbel" {
				return []domain.VocabularyEntry{
					{Term: "humble", TermNorm: "humble", Kind: domain.VocabKindTrack, MatchScore: 0.8},
				}, nil
			}
			return nil, nil
		},
	}
	svc := NewCorrectionService(store)

	result := svc.CorrectAggressive(context.Background(), "kendrick humbel")
	if result == nil || result.Corrected != "kendrick humble" {
		t.Fatalf("want token correction, got %+v", result)
	}

	event := loggedEvent(t, buf, "correction.token")
	if strings.Contains(event, "humbel") {
		t.Fatalf("token correction log leaks raw token text:\n%s", event)
	}
	if !strings.Contains(event, `"fp"`) {
		t.Fatalf("expected a search-text fingerprint in place of the raw token:\n%s", event)
	}
}
