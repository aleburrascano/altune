package resilience_test

import (
	"altune/go-api/internal/shared/resilience"
	"context"
	"testing"
)

func TestMarked_IsFalseUntilAContextIsMarked(t *testing.T) {
	ctx := context.Background()
	if resilience.IsRetrySafe(ctx) {
		t.Fatal("a fresh context reads as marked, so every POST would be repeated")
	}
	if !resilience.IsRetrySafe(resilience.MarkRetrySafe(ctx)) {
		t.Fatal("a marked context reads as unmarked")
	}
}

func TestMarked_SurvivesDerivedContexts(t *testing.T) {
	derived, cancel := context.WithCancel(resilience.MarkRetrySafe(context.Background()))
	defer cancel()
	if !resilience.IsRetrySafe(derived) {
		t.Fatal("the mark is lost on a derived context, so a timeout wrapper would disable the retry")
	}
}
