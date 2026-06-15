package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// helpers (search-music-specific; reuse trackResult/albumResult from dedup_test.go)
// ---------------------------------------------------------------------------

func smTestQuery(t *testing.T, raw string, kinds map[domain.ResultKind]bool, limit int) *domain.SearchQuery {
	t.Helper()
	q, err := domain.NewSearchQuery(raw, "", kinds, limit)
	if err != nil {
		t.Fatalf("NewSearchQuery(%q): %v", raw, err)
	}
	return q
}

func smTrackKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{domain.ResultKindTrack: true}
}

func smUserID() shared.UserId {
	return shared.NewUserId(uuid.New())
}

func smNewService(providers []ports.SearchProvider, historyRepo ports.SearchHistoryRepository, cb *CircuitBreaker) *SearchMusicService {
	if cb == nil {
		cb = NewCircuitBreaker()
	}
	return NewSearchMusicService(providers, nil, historyRepo, cb)
}

// ---------------------------------------------------------------------------
// Execute tests
// ---------------------------------------------------------------------------

func TestSearchMusicService_SingleProvider(t *testing.T) {
	// Arrange
	provider := &mockSearchProvider{
		name:           domain.ProviderDeezer,
		supportedKinds: smTrackKinds(),
		results: []domain.SearchResult{
			trackResult(domain.ProviderDeezer, "d1", "Creep", "Radiohead", map[string]any{}),
			trackResult(domain.ProviderDeezer, "d2", "Karma Police", "Radiohead", map[string]any{}),
		},
	}

	svc := smNewService([]ports.SearchProvider{provider}, nil, nil)
	query := smTestQuery(t, "Radiohead", smTrackKinds(), 10)

	// Act
	out, err := svc.Execute(context.Background(), smUserID(), query, false)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(out.Results) == 0 {
		t.Fatal("expected at least one result, got 0")
	}
	if len(out.ProviderStatuses) != 1 {
		t.Fatalf("expected 1 provider status, got %d", len(out.ProviderStatuses))
	}
	if out.ProviderStatuses[0].Status != domain.ProviderStatusOK {
		t.Errorf("expected provider status OK, got %s", out.ProviderStatuses[0].Status.String())
	}
	if out.Partial {
		t.Error("expected partial=false when all providers succeed")
	}
}

func TestSearchMusicService_MultipleProviders(t *testing.T) {
	// Arrange: two providers return the same track (same ISRC) -> should dedup/merge
	providerA := &mockSearchProvider{
		name:           domain.ProviderDeezer,
		supportedKinds: smTrackKinds(),
		results: []domain.SearchResult{
			trackResult(domain.ProviderDeezer, "d1", "Creep", "Radiohead", map[string]any{"isrc": "GBAYE0000123"}),
		},
	}
	providerB := &mockSearchProvider{
		name:           domain.ProviderMusicBrainz,
		supportedKinds: smTrackKinds(),
		results: []domain.SearchResult{
			trackResult(domain.ProviderMusicBrainz, "mb1", "Creep", "Radiohead", map[string]any{"isrc": "GBAYE0000123"}),
		},
	}

	svc := smNewService([]ports.SearchProvider{providerA, providerB}, nil, nil)
	query := smTestQuery(t, "Creep Radiohead", smTrackKinds(), 10)

	// Act
	out, err := svc.Execute(context.Background(), smUserID(), query, false)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(out.Results) != 1 {
		t.Fatalf("expected 1 merged result (ISRC dedup), got %d", len(out.Results))
	}
	if len(out.Results[0].Sources) < 2 {
		t.Errorf("expected merged result to have >=2 sources, got %d", len(out.Results[0].Sources))
	}
}

func TestSearchMusicService_ProviderTimeout(t *testing.T) {
	// Arrange: provider that waits for context and then returns DeadlineExceeded
	slowProvider := &mockSearchProvider{
		name:           domain.ProviderDeezer,
		supportedKinds: smTrackKinds(),
		searchFn: func(ctx context.Context, _ string, _ map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
			// Wait until the per-provider 1500ms timeout fires
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	svc := smNewService([]ports.SearchProvider{slowProvider}, nil, nil)
	query := smTestQuery(t, "Radiohead", smTrackKinds(), 10)

	// Act
	out, err := svc.Execute(context.Background(), smUserID(), query, false)

	// Assert
	if err != nil {
		t.Fatalf("expected no error (graceful degradation), got %v", err)
	}
	if !out.Partial {
		t.Error("expected partial=true when a provider times out")
	}
	foundTimeout := false
	for _, st := range out.ProviderStatuses {
		if st.Status == domain.ProviderStatusTimeout {
			foundTimeout = true
		}
	}
	if !foundTimeout {
		t.Error("expected at least one provider status to be 'timeout'")
	}
}

func TestSearchMusicService_ProviderError(t *testing.T) {
	// Arrange
	errProvider := &mockSearchProvider{
		name:           domain.ProviderDeezer,
		supportedKinds: smTrackKinds(),
		err:            errors.New("upstream 500"),
	}

	cb := NewCircuitBreaker()
	svc := smNewService([]ports.SearchProvider{errProvider}, nil, cb)
	query := smTestQuery(t, "Radiohead", smTrackKinds(), 10)

	// Act
	out, err := svc.Execute(context.Background(), smUserID(), query, false)

	// Assert
	if err != nil {
		t.Fatalf("expected no error (graceful degradation), got %v", err)
	}
	if !out.Partial {
		t.Error("expected partial=true when a provider errors")
	}
	foundError := false
	for _, st := range out.ProviderStatuses {
		if st.Status == domain.ProviderStatusError {
			foundError = true
		}
	}
	if !foundError {
		t.Error("expected at least one provider status to be 'error'")
	}
}

func TestSearchMusicService_CircuitOpen(t *testing.T) {
	// Arrange: trip the circuit breaker before calling Execute
	provider := &mockSearchProvider{
		name:           domain.ProviderDeezer,
		supportedKinds: smTrackKinds(),
		results: []domain.SearchResult{
			trackResult(domain.ProviderDeezer, "d1", "Test", "Artist", map[string]any{}),
		},
	}

	cb := NewCircuitBreaker()
	for i := 0; i < 10; i++ {
		cb.RecordFailure(domain.ProviderDeezer)
	}

	svc := smNewService([]ports.SearchProvider{provider}, nil, cb)
	query := smTestQuery(t, "Test Artist", smTrackKinds(), 10)

	// Act
	out, err := svc.Execute(context.Background(), smUserID(), query, false)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(out.Results) != 0 {
		t.Errorf("expected 0 results when circuit is open, got %d", len(out.Results))
	}
	foundCircuitOpen := false
	for _, st := range out.ProviderStatuses {
		if st.Status == domain.ProviderStatusCircuitOpen {
			foundCircuitOpen = true
		}
	}
	if !foundCircuitOpen {
		t.Error("expected provider status 'circuit_open' when circuit breaker is open")
	}
}

func TestSearchMusicService_ResultsLimitedToQueryLimit(t *testing.T) {
	// Arrange: provider returns many distinct results (unique ISRCs prevent merging)
	var results []domain.SearchResult
	for i := 0; i < 20; i++ {
		results = append(results, trackResult(
			domain.ProviderDeezer,
			fmt.Sprintf("d%d", i),
			"Track",
			"Radiohead",
			map[string]any{"isrc": fmt.Sprintf("ISRC%04d", i)},
		))
	}

	provider := &mockSearchProvider{
		name:           domain.ProviderDeezer,
		supportedKinds: smTrackKinds(),
		results:        results,
	}

	queryLimit := 5
	svc := smNewService([]ports.SearchProvider{provider}, nil, nil)
	query := smTestQuery(t, "Track Radiohead", smTrackKinds(), queryLimit)

	// Act
	out, err := svc.Execute(context.Background(), smUserID(), query, false)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(out.Results) > queryLimit {
		t.Errorf("expected at most %d results, got %d", queryLimit, len(out.Results))
	}
}

func TestSearchMusicService_SavesHistory(t *testing.T) {
	// Arrange
	provider := &mockSearchProvider{
		name:           domain.ProviderDeezer,
		supportedKinds: smTrackKinds(),
		results: []domain.SearchResult{
			trackResult(domain.ProviderDeezer, "d1", "Creep", "Radiohead", map[string]any{}),
		},
	}

	var insertedEntry *domain.SearchHistoryEntry
	historyRepo := &fakeSearchHistoryRepository{
		insertFn: func(_ context.Context, entry *domain.SearchHistoryEntry) error {
			insertedEntry = entry
			return nil
		},
	}

	userID := smUserID()
	svc := smNewService([]ports.SearchProvider{provider}, historyRepo, nil)
	query := smTestQuery(t, "Radiohead Creep", smTrackKinds(), 10)

	// Act
	_, err := svc.Execute(context.Background(), userID, query, true)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if insertedEntry == nil {
		t.Fatal("expected history entry to be inserted, but insertFn was never called")
	}
	if insertedEntry.Query != "Radiohead Creep" {
		t.Errorf("expected query 'Radiohead Creep', got %q", insertedEntry.Query)
	}
	if insertedEntry.UserId != userID {
		t.Errorf("expected userId %v, got %v", userID, insertedEntry.UserId)
	}
}

func TestSearchMusicService_SkipsHistoryWhenFalse(t *testing.T) {
	// Arrange
	provider := &mockSearchProvider{
		name:           domain.ProviderDeezer,
		supportedKinds: smTrackKinds(),
		results: []domain.SearchResult{
			trackResult(domain.ProviderDeezer, "d1", "Creep", "Radiohead", map[string]any{}),
		},
	}

	insertCalled := false
	historyRepo := &fakeSearchHistoryRepository{
		insertFn: func(_ context.Context, _ *domain.SearchHistoryEntry) error {
			insertCalled = true
			return nil
		},
	}

	svc := smNewService([]ports.SearchProvider{provider}, historyRepo, nil)
	query := smTestQuery(t, "Radiohead Creep", smTrackKinds(), 10)

	// Act
	_, err := svc.Execute(context.Background(), smUserID(), query, false)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if insertCalled {
		t.Error("expected history insert NOT to be called when saveHistory=false")
	}
}

func TestSearchMusicService_HistoryErrorDoesNotFail(t *testing.T) {
	// Arrange
	provider := &mockSearchProvider{
		name:           domain.ProviderDeezer,
		supportedKinds: smTrackKinds(),
		results: []domain.SearchResult{
			trackResult(domain.ProviderDeezer, "d1", "Creep", "Radiohead", map[string]any{}),
		},
	}

	historyRepo := &fakeSearchHistoryRepository{
		insertFn: func(_ context.Context, _ *domain.SearchHistoryEntry) error {
			return errors.New("database unavailable")
		},
	}

	svc := smNewService([]ports.SearchProvider{provider}, historyRepo, nil)
	query := smTestQuery(t, "Radiohead Creep", smTrackKinds(), 10)

	// Act
	out, err := svc.Execute(context.Background(), smUserID(), query, true)

	// Assert
	if err != nil {
		t.Fatalf("expected no error despite history failure, got %v", err)
	}
	if len(out.Results) == 0 {
		t.Error("expected search results to be returned despite history insert failure")
	}
}

// ---------------------------------------------------------------------------
// Enrichment tests
// ---------------------------------------------------------------------------

func TestSearchMusicService_EnrichPopularity(t *testing.T) {
	// Arrange
	provider := &mockSearchProvider{
		name:           domain.ProviderDeezer,
		supportedKinds: smTrackKinds(),
		results: []domain.SearchResult{
			trackResult(domain.ProviderDeezer, "d1", "Creep", "Radiohead", map[string]any{}),
		},
	}

	svc := smNewService([]ports.SearchProvider{provider}, nil, nil)
	svc.SetPopularityResolver(&mockPopularityResolver{
		getPopularityFn: func(_ context.Context, _, _ string) (int64, error) {
			return 85, nil
		},
	})

	query := smTestQuery(t, "Creep Radiohead", smTrackKinds(), 10)

	// Act
	out, err := svc.Execute(context.Background(), smUserID(), query, false)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(out.Results) == 0 {
		t.Fatal("expected at least one result")
	}
	pop, ok := out.Results[0].Extras["popularity"]
	if !ok {
		t.Fatal("expected 'popularity' key in extras after enrichment")
	}
	popVal, ok := pop.(int64)
	if !ok {
		t.Fatalf("expected popularity to be int64, got %T", pop)
	}
	if popVal != 85 {
		t.Errorf("expected popularity=85, got %d", popVal)
	}
}

func TestSearchMusicService_EnrichArtwork_CacheHit(t *testing.T) {
	// Arrange: result with no image, cache returns a URL
	r := trackResult(domain.ProviderDeezer, "d1", "Creep", "Radiohead", map[string]any{})
	r.ImageURL = ""

	provider := &mockSearchProvider{
		name:           domain.ProviderDeezer,
		supportedKinds: smTrackKinds(),
		results:        []domain.SearchResult{r},
	}

	svc := smNewService([]ports.SearchProvider{provider}, nil, nil)
	svc.SetArtworkCache(&mockArtworkCache{
		getFn: func(_ context.Context, _ domain.ResultKind, _, _, _ string) (string, bool, error) {
			return "https://cache.example.com/art.jpg", true, nil
		},
	})
	// Fanart resolver set but should not be needed when cache hits
	svc.SetFanartResolver(&mockArtworkResolver{
		resolveFn: func(_ context.Context, _ domain.ResultKind, _, _, _ string) (string, error) {
			return "", nil
		},
	})

	query := smTestQuery(t, "Creep Radiohead", smTrackKinds(), 10)

	// Act
	out, err := svc.Execute(context.Background(), smUserID(), query, false)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(out.Results) == 0 {
		t.Fatal("expected at least one result")
	}
	if out.Results[0].ImageURL != "https://cache.example.com/art.jpg" {
		t.Errorf("expected cached artwork URL 'https://cache.example.com/art.jpg', got %q", out.Results[0].ImageURL)
	}
}

func TestSearchMusicService_EnrichArtwork_ResolveFanart(t *testing.T) {
	// Arrange: result with no image, no cache, fanart resolver succeeds via mbid
	r := trackResult(domain.ProviderDeezer, "d1", "Creep", "Radiohead", map[string]any{"mbid": "some-mbid-123"})
	r.ImageURL = ""

	provider := &mockSearchProvider{
		name:           domain.ProviderDeezer,
		supportedKinds: smTrackKinds(),
		results:        []domain.SearchResult{r},
	}

	svc := smNewService([]ports.SearchProvider{provider}, nil, nil)
	svc.SetFanartResolver(&mockArtworkResolver{
		resolveFn: func(_ context.Context, _ domain.ResultKind, _, _, mbid string) (string, error) {
			if mbid == "some-mbid-123" {
				return "https://fanart.tv/art.jpg", nil
			}
			return "", nil
		},
	})

	query := smTestQuery(t, "Creep Radiohead", smTrackKinds(), 10)

	// Act
	out, err := svc.Execute(context.Background(), smUserID(), query, false)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(out.Results) == 0 {
		t.Fatal("expected at least one result")
	}
	if out.Results[0].ImageURL != "https://fanart.tv/art.jpg" {
		t.Errorf("expected fanart artwork URL 'https://fanart.tv/art.jpg', got %q", out.Results[0].ImageURL)
	}
}

func TestSearchMusicService_EnrichArtwork_ResolveChain(t *testing.T) {
	// Arrange: no fanart, genius returns empty, chain resolver returns URL
	r := trackResult(domain.ProviderDeezer, "d1", "Creep", "Radiohead", map[string]any{})
	r.ImageURL = ""

	provider := &mockSearchProvider{
		name:           domain.ProviderDeezer,
		supportedKinds: smTrackKinds(),
		results:        []domain.SearchResult{r},
	}

	svc := smNewService([]ports.SearchProvider{provider}, nil, nil)
	svc.SetGeniusResolver(&mockArtworkResolver{
		resolveFn: func(_ context.Context, _ domain.ResultKind, _, _, _ string) (string, error) {
			return "", nil // genius finds nothing
		},
	})
	svc.SetArtworkResolver(&mockArtworkResolver{
		resolveFn: func(_ context.Context, _ domain.ResultKind, _, _, _ string) (string, error) {
			return "https://chain.example.com/fallback.jpg", nil
		},
	})

	query := smTestQuery(t, "Creep Radiohead", smTrackKinds(), 10)

	// Act
	out, err := svc.Execute(context.Background(), smUserID(), query, false)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(out.Results) == 0 {
		t.Fatal("expected at least one result")
	}
	if out.Results[0].ImageURL != "https://chain.example.com/fallback.jpg" {
		t.Errorf("expected chain fallback URL 'https://chain.example.com/fallback.jpg', got %q", out.Results[0].ImageURL)
	}
}

func TestSearchMusicService_NoEnrichmentWithoutResolvers(t *testing.T) {
	// Arrange: no resolvers set at all
	r := trackResult(domain.ProviderDeezer, "d1", "Creep", "Radiohead", map[string]any{})
	r.ImageURL = "https://original.example.com/art.jpg"

	provider := &mockSearchProvider{
		name:           domain.ProviderDeezer,
		supportedKinds: smTrackKinds(),
		results:        []domain.SearchResult{r},
	}

	svc := smNewService([]ports.SearchProvider{provider}, nil, nil)
	query := smTestQuery(t, "Creep Radiohead", smTrackKinds(), 10)

	// Act
	out, err := svc.Execute(context.Background(), smUserID(), query, false)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(out.Results) == 0 {
		t.Fatal("expected at least one result")
	}
	if out.Results[0].ImageURL != "https://original.example.com/art.jpg" {
		t.Errorf("expected original image URL preserved, got %q", out.Results[0].ImageURL)
	}
}
