package service

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"strings"
	"testing"
)

func TestPickBestCorrection_DoesNotLogRawQueryText(t *testing.T) {
	buf := captureLogs(t)

	store := &fakeVocabularyStore{
		findClosestFn: func(_ string, _ int) ([]domain.VocabularyEntry, error) {
			return []domain.VocabularyEntry{
				{Term: "humble", TermNorm: "humble", Kind: domain.VocabKindTrack, MatchScore: 0.8},
			}, nil
		},
	}
	svc := NewCorrectionService(store)

	result := svc.CorrectAggressive(context.Background(), "humbel")
	if result == nil || result.Corrected != "humble" {
		t.Fatalf("want whole-query correction, got %+v", result)
	}

	event := loggedEvent(t, buf, "correction.candidate")
	if strings.Contains(event, "humbel") {
		t.Fatalf("candidate log leaks raw query text:\n%s", event)
	}
	if !strings.Contains(event, `"fp"`) {
		t.Fatalf("expected a search-text fingerprint in place of the raw query:\n%s", event)
	}
}
