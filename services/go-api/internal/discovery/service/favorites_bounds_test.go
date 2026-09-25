package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

type typedHTTPError interface {
	HTTPStatus() int
	ErrorCode() string
}

func assertTypedError(t *testing.T, err error, wantStatus int, wantCode string) {
	t.Helper()
	var typed typedHTTPError
	if !errors.As(err, &typed) {
		t.Fatalf("err = %v, want a typed %d %s", err, wantStatus, wantCode)
	}
	if typed.HTTPStatus() != wantStatus || typed.ErrorCode() != wantCode {
		t.Fatalf("err = %d %s, want %d %s", typed.HTTPStatus(), typed.ErrorCode(), wantStatus, wantCode)
	}
}

func TestFavoritesAdd_RejectsOversizeOrUnsafeFieldsWithoutWriting(t *testing.T) {
	longText := strings.Repeat("é", maxFavoriteTextRunes+1)
	longURL := "https://img.example/" + strings.Repeat("a", maxFavoriteImageURLBytes)
	cases := map[string]domain.Favorite{
		"title over cap":       {Kind: domain.ResultKindAlbum, Title: longText, Subtitle: "Kendrick Lamar"},
		"subtitle over cap":    {Kind: domain.ResultKindAlbum, Title: "DAMN.", Subtitle: longText},
		"image_url over cap":   {Kind: domain.ResultKindAlbum, Title: "DAMN.", ImageURL: longURL},
		"image_url http":       {Kind: domain.ResultKindAlbum, Title: "DAMN.", ImageURL: "http://img.example/a.jpg"},
		"image_url script":     {Kind: domain.ResultKindAlbum, Title: "DAMN.", ImageURL: "javascript:alert(1)"},
		"image_url no host":    {Kind: domain.ResultKindAlbum, Title: "DAMN.", ImageURL: "https:///a.jpg"},
		"empty normalized key": {Kind: domain.ResultKindArtist, Title: "!!!"},
	}
	for name, fav := range cases {
		t.Run(name, func(t *testing.T) {
			repo := &fakeFavoritesRepo{}
			err := NewFavoritesService(repo).Add(context.Background(), shared.UserId{}, fav)

			assertTypedError(t, err, http.StatusBadRequest, "discovery.invalid_favorite")
			if len(repo.added) != 0 {
				t.Errorf("repo.Add called %d times for a rejected favorite", len(repo.added))
			}
		})
	}
}

func TestFavoritesAdd_AcceptsFieldsAtTheirCaps(t *testing.T) {
	repo := &fakeFavoritesRepo{}
	fav := domain.Favorite{
		Kind:     domain.ResultKindAlbum,
		Title:    strings.Repeat("é", maxFavoriteTextRunes),
		Subtitle: strings.Repeat("b", maxFavoriteTextRunes),
		ImageURL: "https://img.example/" + strings.Repeat("a", maxFavoriteImageURLBytes-len("https://img.example/")),
	}

	if err := NewFavoritesService(repo).Add(context.Background(), shared.UserId{}, fav); err != nil {
		t.Fatalf("Add at the caps: %v", err)
	}
	if len(repo.added) != 1 {
		t.Fatalf("repo.Add called %d times, want 1", len(repo.added))
	}
}

func TestFavoritesRemove_OversizeTitleStillDeletes(t *testing.T) {
	repo := &fakeFavoritesRepo{}
	err := NewFavoritesService(repo).Remove(context.Background(), shared.UserId{},
		domain.ResultKindAlbum, strings.Repeat("a", maxFavoriteTextRunes+1), "")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(repo.removed) != 1 {
		t.Errorf("repo.Remove called %d times, want 1", len(repo.removed))
	}
}

type fullFavoritesRepo struct{ fakeFavoritesRepo }

func (f *fullFavoritesRepo) Add(context.Context, shared.UserId, domain.Favorite) error {
	return fmt.Errorf("insert favorite: %w", ports.ErrFavoritesFull)
}

func TestFavoritesAdd_OverThePerUserCapIsATypedConflict(t *testing.T) {
	err := NewFavoritesService(&fullFavoritesRepo{}).Add(context.Background(), shared.UserId{},
		domain.Favorite{Kind: domain.ResultKindAlbum, Title: "DAMN.", Subtitle: "Kendrick Lamar"})

	assertTypedError(t, err, http.StatusConflict, "discovery.favorites_full")
}
