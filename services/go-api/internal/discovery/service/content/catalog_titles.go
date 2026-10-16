package content

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/discovery/service/providercall"
	"altune/go-api/internal/shared/fanout"
	"altune/go-api/internal/shared/redact"
	"context"
	"log/slog"
	"sync"
)

var _ ports.ArtistCatalogTitles = (*GetArtistContentService)(nil)

const (
	catalogTitlesTrackLimit       = 50
	catalogTitlesAlbumLimit       = 200
	catalogAlbumTracksConcurrency = 8
)

func (s *GetArtistContentService) CatalogTitles(ctx context.Context, provider domain.ProviderName, externalID string, known map[domain.ProviderName]string) ([]string, error) {
	tracks, err := s.GetTopTracks(ctx, provider, externalID, known, catalogTitlesTrackLimit)
	if err != nil {
		return nil, err
	}
	albums, err := s.GetAlbums(ctx, provider, externalID, known, catalogTitlesAlbumLimit)
	if err != nil {
		return nil, err
	}
	var items []domain.SearchResult
	for _, resp := range []*providercall.ContentFetchResponse{tracks, albums} {
		if resp != nil {
			items = append(items, resp.Items...)
		}
	}
	albumTracks, tracklistsComplete := s.albumTracklistTitles(ctx, albumsOf(albums))
	titles := make([]string, 0, len(items)+len(albumTracks))
	for _, item := range items {
		titles = append(titles, item.Title)
	}
	titles = append(titles, albumTracks...)
	if !tracklistsComplete || isPartial(tracks) || isPartial(albums) {
		return titles, ports.ErrPartialCatalog
	}
	return titles, nil
}

func isPartial(resp *providercall.ContentFetchResponse) bool {
	return resp != nil && resp.Partial
}

func albumsOf(resp *providercall.ContentFetchResponse) []domain.SearchResult {
	if resp == nil {
		return nil
	}
	return resp.Items
}

type tracklistTitles struct {
	mu       sync.Mutex
	titles   []string
	complete bool
}

func (t *tracklistTitles) add(tracks []domain.SearchResult) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, track := range tracks {
		t.titles = append(t.titles, track.Title)
	}
}

func (t *tracklistTitles) markIncomplete() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.complete = false
}

func (s *GetArtistContentService) albumTracklistTitles(ctx context.Context, albums []domain.SearchResult) (titles []string, complete bool) {
	collected := &tracklistTitles{complete: true}
	g := fanout.New(catalogAlbumTracksConcurrency)
	for _, album := range albums {
		if source, lister, ok := s.tracklistSource(album); ok {
			g.Go(func() { fetchTracklist(ctx, source, lister, collected) })
		}
	}
	g.Wait()
	return collected.titles, collected.complete
}

func fetchTracklist(ctx context.Context, source domain.SourceRef, lister ports.AlbumContentProvider, into *tracklistTitles) {
	tracks, err := lister.GetAlbumTracks(ctx, source.Provider, source.ExternalID)
	if err != nil {
		into.markIncomplete()
		slog.WarnContext(ctx, "catalog_titles.album_tracks_failed",
			"provider", source.Provider.String(), "album_id", source.ExternalID, "error", redact.Secrets(err.Error()))
		return
	}
	into.add(tracks)
}

func (s *GetArtistContentService) tracklistSource(album domain.SearchResult) (domain.SourceRef, ports.AlbumContentProvider, bool) {
	for _, source := range album.Sources {
		if source.ExternalID == "" {
			continue
		}
		if lister, ok := s.providers[source.Provider].(ports.AlbumContentProvider); ok {
			return source, lister, true
		}
	}
	return domain.SourceRef{}, nil, false
}
