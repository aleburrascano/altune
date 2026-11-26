package persistence_test

import (
	"altune/go-api/internal/search/internal/adapters/persistence"
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/testdb"
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func favoritesRepoForUser(t *testing.T) (*pgxpool.Pool, *persistence.PostgresFavoritesRepository, ids.UserId) {
	t.Helper()
	pool := testdb.Pool(t)
	userId := ids.NewUserId(uuid.New())
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM discovery_favorites WHERE user_id = $1`, userId.UUID())
	})
	return pool, persistence.NewPostgresFavoritesRepository(pool), userId
}

func seedFavorites(t *testing.T, pool *pgxpool.Pool, userId ids.UserId, count int) {
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

func countFavorites(t *testing.T, pool *pgxpool.Pool, userId ids.UserId) int {
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

func TestPostgresFavoritesRepo_AddRefusesANewRowAtThePerUserCap(t *testing.T) {
	testdb.RequireIntegration(t)
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

func TestPostgresFavoritesRepo_AddUpdatesAnExistingRowAtTheCap(t *testing.T) {
	testdb.RequireIntegration(t)
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

func TestPostgresFavoritesRepo_AddBelowTheCapInserts(t *testing.T) {
	testdb.RequireIntegration(t)
	pool, repo, userId := favoritesRepoForUser(t)
	seedFavorites(t, pool, userId, ports.MaxFavoritesPerUser-1)

	if err := repo.Add(context.Background(), userId, albumFavorite("last slot")); err != nil {
		t.Fatalf("Add below the cap: %v", err)
	}
	if got := countFavorites(t, pool, userId); got != ports.MaxFavoritesPerUser {
		t.Fatalf("rows = %d, want %d", got, ports.MaxFavoritesPerUser)
	}
}

func TestPostgresFavoritesRepo_ListForUserReadsAtMostTheCap(t *testing.T) {
	testdb.RequireIntegration(t)
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

func TestPostgresFavoritesRepo_ListForUserSkipsAKindItDoesNotKnowAndKeepsNewestFirst(t *testing.T) {
	pool, repo, userId := favoritesRepoForUser(t)
	ctx := context.Background()
	if got, err := repo.ListForUser(ctx, userId); err != nil || got == nil || len(got) != 0 {
		t.Fatalf("ListForUser with no favorites = %#v, %v; want an empty, non-nil list", got, err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO discovery_favorites (user_id, kind, entity_key, title, created_at) VALUES
		($1, 'album', 'a', 'A', now()),
		($1, 'bogus', 'b', 'B', now() - interval '1 minute'),
		($1, 'artist', 'c', 'C', now() - interval '2 minute')`, userId.UUID()); err != nil {
		t.Fatalf("seed favorites: %v", err)
	}

	got, err := repo.ListForUser(ctx, userId)

	if err != nil || len(got) != 2 || got[0].Key != "a" || got[0].Title != "A" || got[1].Key != "c" {
		t.Fatalf("ListForUser = %#v, %v; want album a then artist c, without the unknown kind", got, err)
	}
}
