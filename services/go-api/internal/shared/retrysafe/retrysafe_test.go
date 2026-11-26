package retrysafe_test

import (
	"altune/go-api/internal/shared/retrysafe"
	"context"
	"testing"
)

func TestMarked_IsFalseUntilAContextIsMarked(t *testing.T) {
	ctx := context.Background()
	if retrysafe.Marked(ctx) {
		t.Fatal("a fresh context reads as marked, so every POST would be repeated")
	}
	if !retrysafe.Marked(retrysafe.Mark(ctx)) {
		t.Fatal("a marked context reads as unmarked")
	}
}

func TestMarked_SurvivesDerivedContexts(t *testing.T) {
	derived, cancel := context.WithCancel(retrysafe.Mark(context.Background()))
	defer cancel()
	if !retrysafe.Marked(derived) {
		t.Fatal("the mark is lost on a derived context, so a timeout wrapper would disable the retry")
	}
}
