package handler

import (
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"context"
	"net/http"
	"testing"
	"time"

	discdomain "altune/go-api/internal/discovery/domain"
)

// These tests guard issue #568: a panic inside a goroutine the handler spawns
// must be contained. Without recovery each one crashes the test binary.

type panickingArtistContentProvider struct{}

func (panickingArtistContentProvider) GetArtistTopTracks(context.Context, discdomain.ProviderName, string) ([]discdomain.SearchResult, error) {
	panic("top tracks exploded")
}

func (panickingArtistContentProvider) GetArtistAlbums(context.Context, discdomain.ProviderName, string) ([]discdomain.SearchResult, error) {
	panic("albums exploded")
}

func TestHandleArtistContent_PanickingProviderReturns500(t *testing.T) {
	artistProviders := map[discdomain.ProviderName]ports.ArtistContentProvider{
		discdomain.ProviderDeezer: panickingArtistContentProvider{},
	}
	router := buildDiscoveryRouter(nil, &fakeSearchHistoryRepo{}, nil, artistProviders)

	rec := discServe(t, router, http.MethodGet, "/discovery/artists/deezer/1/content", nil)
	discAssertStatus(t, rec, http.StatusInternalServerError)
}

type stubOwnershipReader struct{}

func (stubOwnershipReader) OwnedByTitleArtist(context.Context, shared.UserId) (map[string]ports.OwnedTrack, error) {
	return nil, nil
}

type panickingTrackNumberFiller struct{ called chan struct{} }

func (f panickingTrackNumberFiller) FillTrackNumber(context.Context, shared.UserId, string, int) error {
	defer close(f.called)
	panic("track number fill exploded")
}

func TestFillAlbumTrackNumbers_PanickingFillerIsContained(t *testing.T) {
	filler := panickingTrackNumberFiller{called: make(chan struct{})}
	h := NewDiscoveryHandler(DiscoveryServices{}).
		WithOwnership(stubOwnershipReader{}).
		WithTrackNumberFiller(filler)
	items := []SearchResultDTO{{Extras: map[string]any{"owned_track_id": "track-1"}}}

	h.fillAlbumTrackNumbers(context.Background(), shared.UserId{}, items)

	select {
	case <-filler.called:
	case <-time.After(2 * time.Second):
		t.Fatal("filler was never called")
	}
	// An unrecovered panic in the detached goroutine terminates the process
	// immediately after the filler unwinds; give it room to do so.
	time.Sleep(100 * time.Millisecond)
}
