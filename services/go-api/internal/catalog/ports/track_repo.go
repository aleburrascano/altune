package ports

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"errors"
)

// ErrTrackVersionConflict is the optimistic-lock CAS miss surfaced by
// TrackUpdater.Update: the row was found and owned, but its version had already
// advanced past the expected one, so a concurrent writer settled it first
// (#1419). It is deliberately distinct from the "not found or was deleted"
// error so a caller can tell "someone else won the race, reload and decide"
// apart from "the row is gone", and retry or report rather than silently losing
// the update.
var ErrTrackVersionConflict = errors.New("track version conflict")

// The track lifecycle is served by one persistence adapter but consumed through
// narrow per-capability ports: each catalog service depends only on the methods
// it calls, so a test double or decorator implements just that slice.

// TrackAdder inserts a track, returning the stored row and whether it was newly
// created (false when an idempotency or dedup match already existed).
type TrackAdder interface {
	Add(ctx context.Context, track *domain.Track) (stored *domain.Track, created bool, err error)
}

// TrackGetter reads one owner-scoped track.
type TrackGetter interface {
	GetByID(ctx context.Context, id domain.TrackId, userId shared.UserId) (*domain.Track, error)
}

// TrackBatchGetter reads the owner-scoped tracks among the given ids.
type TrackBatchGetter interface {
	ListByIDs(ctx context.Context, userId shared.UserId, ids []domain.TrackId) ([]*domain.Track, error)
}

// TrackCounter counts one user's tracks, stopping at atMost so the answer
// costs the same whatever the library holds. A result of atMost means "at
// least atMost", which is all a cap check can act on.
type TrackCounter interface {
	CountForUser(ctx context.Context, userId shared.UserId, atMost int) (int, error)
}

// TrackLister pages through a user's tracks.
type TrackLister interface {
	ListForUser(ctx context.Context, userId shared.UserId, limit, offset int) (tracks []*domain.Track, total int, err error)
}

// TrackUpdater persists changes to an existing track under an optimistic-lock
// CAS. expectedVersion is the domain.Track.Version the caller read before
// mutating; the write matches the owned row only at that version and bumps it,
// updating track.Version to the new value on success. A row whose version has
// advanced yields ErrTrackVersionConflict (a concurrent writer won the race),
// distinct from the not-found/deleted error, so the caller can reload and retry
// or report instead of silently clobbering the winner.
type TrackUpdater interface {
	Update(ctx context.Context, track *domain.Track, expectedVersion int) error
}

// TrackNumberSetter sets a track's album position.
//
// SetTrackNumber is write-once: it only fills a track whose number is still
// unset, and never overwrites an existing one. updated=false with a nil error
// means nothing was written, because the number was already set or because no
// track with that id is owned by userId; the two cases are not distinguished.
type TrackNumberSetter interface {
	SetTrackNumber(ctx context.Context, id domain.TrackId, userId shared.UserId, trackNumber int) (updated bool, err error)
}

// TrackDeleter removes one owner-scoped track and returns its audio ref.
type TrackDeleter interface {
	Delete(ctx context.Context, id domain.TrackId, userId shared.UserId) (deleted bool, audioRef *string, err error)
}

// TrackAudioRefLookup answers whether audioRef still serves a track other than
// excludeTrackID. The key is derived from normalized metadata, so tracks with
// equivalent metadata share one object and audio_ref is non-unique: a delete
// that skips this check can strip a remaining track of its file (#2203). The
// question spans every owner, matching acquisition ports.AudioRefLookup.
type TrackAudioRefLookup interface {
	AudioRefInUse(ctx context.Context, audioRef string, excludeTrackID domain.TrackId) (bool, error)
}

// TrackAudioDeleter deletes an owned track and asks who else holds its audio,
// so only the last reference takes the object with it.
type TrackAudioDeleter interface {
	TrackDeleter
	TrackAudioRefLookup
}

// TrackReadWriter reads a track and writes it back.
type TrackReadWriter interface {
	TrackGetter
	TrackUpdater
}

// TrackNumberFiller sets a track's album position and, when the write-once
// update is a no-op, reads the track back to tell "already set" from "no such
// owned track".
type TrackNumberFiller interface {
	TrackGetter
	TrackNumberSetter
}

// TrackAddUpdater inserts a track, measures the library it lands in against
// the per-user cap, and writes back later changes to it.
type TrackAddUpdater interface {
	TrackAdder
	TrackCounter
	TrackUpdater
}

// TrackLookup reads tracks singly or in batches.
type TrackLookup interface {
	TrackGetter
	TrackBatchGetter
}

// LibraryLensRepository serves grouped, lens-style reads over a user's library.
type LibraryLensRepository interface {
	ListFilteredForUser(ctx context.Context, userId shared.UserId, query domain.LibraryQuery) (tracks []*domain.Track, total int, err error)
	ListAlbumsForUser(ctx context.Context, userId shared.UserId, query domain.LibraryQuery) ([]domain.AlbumGroup, error)
	ListArtistsForUser(ctx context.Context, userId shared.UserId, query domain.LibraryQuery) ([]domain.ArtistGroup, error)
}

// FeaturedArtistRepository manages featured-artist associations on tracks.
type FeaturedArtistRepository interface {
	ListTracksFeaturing(ctx context.Context, userId shared.UserId, fa domain.FeaturedArtist) ([]*domain.Track, error)
	ReplaceFeaturedArtists(ctx context.Context, id domain.TrackId, userId shared.UserId, feats []domain.FeaturedArtist) error
}
