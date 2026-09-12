package persistence

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// blockingPool is a pgxPool whose every call blocks until its context is done,
// then returns that context's error. It stands in for a wedged database: a
// connection the client holds open while the server never answers. With the
// per-call deadline in place a repository call against it must return in ~the
// deadline; without the deadline it would block forever.
type blockingPool struct{}

func (blockingPool) Begin(ctx context.Context) (pgx.Tx, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (blockingPool) Query(ctx context.Context, _ string, _ ...any) (pgx.Rows, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (blockingPool) QueryRow(ctx context.Context, _ string, _ ...any) pgx.Row {
	<-ctx.Done()
	return errRow{err: ctx.Err()}
}

func (blockingPool) Exec(ctx context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	<-ctx.Done()
	return pgconn.CommandTag{}, ctx.Err()
}

type errRow struct{ err error }

func (r errRow) Scan(_ ...any) error { return r.err }

// TestPersistenceAdapters_StuckCallIsBounded proves that a stuck dependency call
// in each catalog persistence adapter is bounded by the per-call deadline rather
// than blocking the handler goroutine (and its pooled connection) indefinitely.
// It exercises every pgxPool entry point (Begin, Query, QueryRow, Exec) across
// all four adapters against a pool that never answers.
func TestPersistenceAdapters_StuckCallIsBounded(t *testing.T) {
	restore := dbCallTimeout
	dbCallTimeout = 50 * time.Millisecond
	t.Cleanup(func() { dbCallTimeout = restore })

	pool := blockingPool{}
	trackRepo := &PgxTrackRepository{pool: pool}
	playlistRepo := &PgxPlaylistRepository{pool: pool}
	featuredRepo := &PgxFeaturedArtistRepository{pool: pool}
	lensRepo := &PgxLibraryLensRepository{pool: pool}

	userId := shared.NewUserId(uuid.New())
	track := newTestTrackForDB(t, userId)
	playlist, err := domain.NewPlaylist(userId, "stuck")
	if err != nil {
		t.Fatalf("NewPlaylist: %v", err)
	}

	cases := []struct {
		name string
		call func(context.Context) error
	}{
		{"track.Query/ListForUser", func(ctx context.Context) error {
			_, _, err := trackRepo.ListForUser(ctx, userId, 10, 0)
			return err
		}},
		{"track.QueryRow/GetByID", func(ctx context.Context) error {
			_, err := trackRepo.GetByID(ctx, track.ID, userId)
			return err
		}},
		{"track.Exec/Update", func(ctx context.Context) error {
			return trackRepo.Update(ctx, track)
		}},
		{"track.Begin/Add", func(ctx context.Context) error {
			_, _, err := trackRepo.Add(ctx, track)
			return err
		}},
		{"playlist.Exec/Create", func(ctx context.Context) error {
			return playlistRepo.Create(ctx, playlist)
		}},
		{"playlist.Begin/AddTrack", func(ctx context.Context) error {
			return playlistRepo.AddTrack(ctx, playlist.ID, track.ID, 0)
		}},
		{"featured.Begin/ReplaceFeaturedArtists", func(ctx context.Context) error {
			return featuredRepo.ReplaceFeaturedArtists(ctx, track.ID, userId, nil)
		}},
		{"lens.Query/ListFilteredForUser", func(ctx context.Context) error {
			_, _, err := lensRepo.ListFilteredForUser(ctx, userId, domain.LibraryQuery{Limit: 10})
			return err
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			done := make(chan error, 1)
			start := time.Now()
			go func() { done <- tc.call(context.Background()) }()

			select {
			case err := <-done:
				if err == nil {
					t.Fatalf("expected a deadline error, got nil")
				}
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("expected context.DeadlineExceeded, got %v", err)
				}
				if elapsed := time.Since(start); elapsed > 2*time.Second {
					t.Fatalf("call returned after %v, far beyond the %v deadline", elapsed, dbCallTimeout)
				}
			case <-time.After(2 * time.Second):
				t.Fatalf("call did not return within 2s — the stuck dependency was not bounded")
			}
		})
	}
}
