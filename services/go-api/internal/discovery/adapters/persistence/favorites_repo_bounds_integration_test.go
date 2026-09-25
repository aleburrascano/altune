//go:build integration

package persistence

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func favoritesRepoForUser(t *testing.T) (*pgxpool.Pool, *PgxFavoritesRepository, shared.UserId) {
	t.Helper()
	pool := testPool(t)
	userId := shared.NewUserId(uuid.New())
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM discovery_favorites WHERE user_id = $1`, userId.UUID())
	})
	return pool, NewPgxFavoritesRepository(pool), userId
}

func seedFavorites(t *testing.T, pool *pgxpool.Pool, userId shared.UserId, count int) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO discovery_favorites (user_id, kind, entity_key, title)
		SELECT $1, 'album', 'seed|' || n, 'seed ' || n FROM generate_series(1, $2::int) AS n`,
		userId.UUID(), count,
	)
	if err != nil {
		t.Fatalf("seed %d favorites: %v", count, err)
	}
}

func countFavorites(t *testing.T, pool *pgxpool.Pool, userId shared.UserId) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM discovery_favorites WHERE user_id = $1`, userId.UUID()).Scan(&n); err != nil {
		t.Fatalf("count favorites: %v", err)
	}
	return n
}

func albumFavorite(title string) domain.Favorite {
	return domain.Favorite{
		Kind:  domain.ResultKindAlbum,
		Key:   domain.FavoriteKey(domain.ResultKindAlbum, title, ""),
		Title: title,
	}
}

func TestPgxFavoritesRepo_AddRefusesANewRowAtThePerUserCap(t *testing.T) {
	pool, repo, userId := favoritesRepoForUser(t)
	seedFavorites(t, pool, userId, ports.MaxFavoritesPerUser)

	err := repo.Add(context.Background(), userId, albumFavorite("one too many"))

	if !errors.Is(err, ports.ErrFavoritesFull) {
		t.Fatalf("Add at the cap = %v, want ErrFavoritesFull", err)
	}
	if got := countFavorites(t, pool, userId); got != ports.MaxFavoritesPerUser {
		t.Fatalf("rows = %d, want %d", got, ports.MaxFavoritesPerUser)
	}
}

func TestPgxFavoritesRepo_AddUpdatesAnExistingRowAtTheCap(t *testing.T) {
	pool, repo, userId := favoritesRepoForUser(t)
	ctx := context.Background()
	fav := albumFavorite("kept")
	if err := repo.Add(ctx, userId, fav); err != nil {
		t.Fatalf("first Add: %v", err)
	}
	seedFavorites(t, pool, userId, ports.MaxFavoritesPerUser-1)

	fav.ImageURL = "https://img.example/new.jpg"
	if err := repo.Add(ctx, userId, fav); err != nil {
		t.Fatalf("re-Add of an existing favorite at the cap: %v", err)
	}
	var imageURL string
	if err := pool.QueryRow(ctx,
		`SELECT image_url FROM discovery_favorites WHERE user_id = $1 AND entity_key = $2`,
		userId.UUID(), fav.Key).Scan(&imageURL); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if imageURL != fav.ImageURL {
		t.Fatalf("image_url = %q, want %q", imageURL, fav.ImageURL)
	}
}

func TestPgxFavoritesRepo_AddBelowTheCapInserts(t *testing.T) {
	pool, repo, userId := favoritesRepoForUser(t)
	seedFavorites(t, pool, userId, ports.MaxFavoritesPerUser-1)

	if err := repo.Add(context.Background(), userId, albumFavorite("last slot")); err != nil {
		t.Fatalf("Add below the cap: %v", err)
	}
	if got := countFavorites(t, pool, userId); got != ports.MaxFavoritesPerUser {
		t.Fatalf("rows = %d, want %d", got, ports.MaxFavoritesPerUser)
	}
}

func TestPgxFavoritesRepo_ListForUserReadsAtMostTheCap(t *testing.T) {
	pool, repo, userId := favoritesRepoForUser(t)
	seedFavorites(t, pool, userId, ports.MaxFavoritesPerUser+5)

	got, err := repo.ListForUser(context.Background(), userId)
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	if len(got) != ports.MaxFavoritesPerUser {
		t.Fatalf("ListForUser returned %d rows, want %d", len(got), ports.MaxFavoritesPerUser)
	}
}
