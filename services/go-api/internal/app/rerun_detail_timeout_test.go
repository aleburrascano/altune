package app

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"testing"
	"time"

	discoveryPorts "altune/go-api/internal/discovery/ports"
	discoveryService "altune/go-api/internal/discovery/service"
)

// hangingProvider simulates a slow upstream that respects cancellation but
// otherwise never returns: it blocks until the context is done. Six of these
// called back-to-back with no aggregate deadline would stall forever.
type hangingProvider struct{}

func (hangingProvider) GetArtistAlbums(ctx context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (hangingProvider) GetArtistTopTracks(ctx context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func slowDetailReRunner() *detailReRunner {
	provs := map[domain.ProviderName]discoveryPorts.ArtistContentProvider{
		domain.ProviderDeezer:     hangingProvider{},
		domain.ProviderSoundCloud: hangingProvider{},
		domain.ProviderITunes:     hangingProvider{},
		domain.ProviderLastFM:     hangingProvider{},
	}
	return &detailReRunner{artistSvc: discoveryService.NewGetArtistContentService(provs)}
}

// TestFanOutSeeds_boundsTotalWallTimeWithSlowProviders reproduces the defect:
// with providers that hang until cancelled and a caller context that carries no
// deadline, the sequential fan-out must still return within an aggregate budget
// rather than stalling indefinitely. Before the aggregate context.WithTimeout
// was added, this test blocks forever on the first provider call.
func TestFanOutSeeds_boundsTotalWallTimeWithSlowProviders(t *testing.T) {
	prev := detailReRunBudget
	detailReRunBudget = 100 * time.Millisecond
	t.Cleanup(func() { detailReRunBudget = prev })

	dr := slowDetailReRunner()
	byProvider := map[string]string{"deezer": "d", "soundcloud": "s", "itunes": "i"}
	entity := domain.SearchResult{Title: "Artist", MBID: "mbid-1"}

	done := make(chan struct{})
	go func() {
		// context.Background carries no deadline: the only bound is the one the
		// fan-out imposes on itself.
		dr.fanOutSeeds(context.Background(), byProvider, entity)
		close(done)
	}()

	// The aggregate budget is 100ms, so a correct fan-out finishes well within
	// this window. A missing aggregate timeout never fires.
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("fanOutSeeds did not return: sequential provider fan-out has no aggregate wall-time bound")
	}
}
