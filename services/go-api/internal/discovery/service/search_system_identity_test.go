package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"context"
	"testing"
)

// The smoke-eval job runs the real per-user search path with a canned query.
// When it ran as the real operator account it read that operator's favorites
// and persisted an InteractionEvent and a history row under their real id,
// contaminating their ranking and behavioral signal. The synthetic system
// identity must skip all three, while a real identity keeps its
// personalization, telemetry, and history.
func TestExecute_SystemIdentityDoesNotTouchFavoritesOrTelemetry(t *testing.T) {
	favorites := []domain.Favorite{favoriteOf(domain.ResultKindArtist, "Kendrick Lamar", "")}
	results := []domain.SearchResult{
		deezerTrack("Humble", "Kendrick Lamar", 80),
		deezerTrack("Humble Beginnings", "Kendrick Lamar", 70),
	}

	t.Run("real identity is personalized and recorded (control)", func(t *testing.T) {
		store, favs, history := runExecuteAs(t, newUser(), favorites, results, true)
		if len(store.snapshot()) == 0 {
			t.Error("real identity should persist an InteractionEvent")
		}
		if favs.listCalls == 0 {
			t.Error("real identity should consult favorites")
		}
		if len(history) == 0 {
			t.Error("real identity should persist its search history")
		}
	})

	t.Run("system identity is neither personalized nor recorded", func(t *testing.T) {
		store, favs, history := runExecuteAs(t, shared.SystemUserId(), favorites, results, true)
		if got := len(store.snapshot()); got != 0 {
			t.Errorf("system identity persisted %d InteractionEvents; want 0", got)
		}
		if favs.listCalls != 0 {
			t.Errorf("system identity consulted favorites %d times; want 0", favs.listCalls)
		}
		if got := len(history); got != 0 {
			t.Errorf("system identity persisted %d history entries; want 0", got)
		}
	})
}

func runExecuteAs(
	t *testing.T,
	user shared.UserId,
	favorites []domain.Favorite,
	results []domain.SearchResult,
	saveHistory bool,
) (*fakeEventStore, *fakeFavoritesRepo, []*domain.SearchHistoryEntry) {
	t.Helper()
	store := &fakeEventStore{}
	favs := &fakeFavoritesRepo{favorites: favorites}
	var recorded []*domain.SearchHistoryEntry
	history := &fakeHistoryWriter{
		insertFn: func(_ context.Context, entry *domain.SearchHistoryEntry) error {
			recorded = append(recorded, entry)
			return nil
		},
	}
	p := &fakeProvider{name: domain.ProviderDeezer, results: results}
	svc := NewService(
		[]ports.SearchProvider{p},
		NewCircuitBreaker(),
		WithEventStore(store),
		WithFavorites(favs),
		WithHistoryRepository(history),
	)
	if _, err := svc.Execute(context.Background(), user, newQuery(t, "humble"), saveHistory); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	svc.WaitForBackground()
	return store, favs, recorded
}
