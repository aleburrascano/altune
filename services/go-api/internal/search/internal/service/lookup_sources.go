package service

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"context"
	"fmt"
	"log/slog"
)

func required(name string) error {
	return fmt.Errorf("%w: %s is nil", domain.ErrRelatedCollaboratorRequired, name)
}

type LookupSource interface {
	resultKind() domain.ResultKind
	dispatch(ctx context.Context, fan *relatedFanOut, userId ids.UserId, result domain.SearchResult)
}

type libraryMatchesSource struct {
	querier ports.RelationshipQuerier
}

func LibraryMatches(querier ports.RelationshipQuerier) (LookupSource, error) {
	if querier == nil {
		return nil, required("querier")
	}
	return libraryMatchesSource{querier: querier}, nil
}

func (libraryMatchesSource) resultKind() domain.ResultKind { return domain.ResultKindTrack }

func (s libraryMatchesSource) dispatch(ctx context.Context, fan *relatedFanOut, userId ids.UserId, result domain.SearchResult) {
	album := result.Album
	if album == "" {
		return
	}
	lookup := libraryMatchesLookup(userId, result)
	if fan.serveMemoized(lookup) {
		return
	}
	fan.fetchRelatedGroup(ctx, lookup, func(ctx context.Context) ([]domain.SearchResult, error) {
		matches, err := s.querier.FindRelatedByAlbumArtist(ctx, userId, album, result.Subtitle, relatedPerGroup)
		if err != nil {
			slog.DebugContext(ctx, "related.library_lookup_failed", logging.KeyError, err)
			return nil, err
		}
		return fan.memoize(lookup, matchesToSearchResults(matches)), nil
	})
}

type albumTracksSource struct {
	provider ports.AlbumContentProvider
	breaker  *CircuitBreaker
}

func AlbumTracksSource(provider ports.AlbumContentProvider, breaker *CircuitBreaker) (LookupSource, error) {
	if provider == nil {
		return nil, required("album provider")
	}
	if breaker == nil {
		return nil, required("breaker")
	}
	return albumTracksSource{provider: provider, breaker: breaker}, nil
}

func (albumTracksSource) resultKind() domain.ResultKind { return domain.ResultKindTrack }

func (s albumTracksSource) dispatch(ctx context.Context, fan *relatedFanOut, _ ids.UserId, result domain.SearchResult) {
	albumID := result.DeezerAlbumID
	if albumID == "" {
		return
	}
	fan.fetchGuardedGroup(ctx, albumTracksLookup(albumID, result), s.breaker, func(ctx context.Context) ([]domain.SearchResult, error) {
		return s.provider.GetAlbumTracks(ctx, domain.CanonicalContentProvider, albumID)
	})
}

type artistAlbumsSource struct {
	provider ports.ArtistContentProvider
	breaker  *CircuitBreaker
}

func ArtistAlbumsSource(provider ports.ArtistContentProvider, breaker *CircuitBreaker) (LookupSource, error) {
	if provider == nil {
		return nil, required("artist provider")
	}
	if breaker == nil {
		return nil, required("breaker")
	}
	return artistAlbumsSource{provider: provider, breaker: breaker}, nil
}

func (artistAlbumsSource) resultKind() domain.ResultKind { return domain.ResultKindArtist }

func (s artistAlbumsSource) dispatch(ctx context.Context, fan *relatedFanOut, _ ids.UserId, result domain.SearchResult) {
	artistID := extractDeezerID(result)
	if artistID == "" {
		return
	}
	fan.fetchGuardedGroup(ctx, artistAlbumsLookup(artistID, result), s.breaker, func(ctx context.Context) ([]domain.SearchResult, error) {
		return s.provider.GetArtistAlbums(ctx, domain.CanonicalContentProvider, artistID)
	})
}

func (f *relatedFanOut) fetchGuardedGroup(
	ctx context.Context,
	lookup relatedGroupLookup,
	breaker *CircuitBreaker,
	call func(ctx context.Context) ([]domain.SearchResult, error),
) {
	if f.serveMemoized(lookup) {
		return
	}
	if !f.reserveProviderCall() {
		return
	}
	f.fetchRelatedGroup(ctx, lookup, func(ctx context.Context) ([]domain.SearchResult, error) {
		items, err := GuardedFetch(ctx, breaker, domain.CanonicalContentProvider, func() ([]domain.SearchResult, error) {
			return call(ctx)
		})
		if IsPartialResult(err) {
			return truncateRelated(items), nil
		}
		if err != nil {
			return nil, fmt.Errorf("guarded fetch: %w", err)
		}
		return f.memoize(lookup, truncateRelated(items)), nil
	})
}
