package catalogtest

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"context"
	"errors"
	"testing"
)

func FeaturedResolverContract(t *testing.T, newResolver func(t *testing.T, feats []domain.FeaturedArtist, err error) ports.FeaturedArtistResolver) {
	ctx := context.Background()

	t.Run("no upstream artists resolves to an empty list", func(t *testing.T) {
		got, err := newResolver(t, nil, nil).Resolve(ctx, "Artist", "Song")
		if err != nil || len(got) != 0 {
			t.Fatalf("Resolve = %v, %v; want no artists, nil", got, err)
		}
	})

	t.Run("returns the upstream artists", func(t *testing.T) {
		got, err := newResolver(t, []domain.FeaturedArtist{{Name: "Guest"}}, nil).Resolve(ctx, "Artist", "Song")
		if err != nil || len(got) != 1 || got[0].Name != "Guest" {
			t.Fatalf("Resolve = %v, %v; want one artist named Guest, nil", got, err)
		}
	})

	t.Run("an upstream error is returned with no artists", func(t *testing.T) {
		wantErr := errors.New("provider down")
		resolver := newResolver(t, []domain.FeaturedArtist{{Name: "Guest"}}, wantErr)

		got, err := resolver.Resolve(ctx, "Artist", "Song")
		if !errors.Is(err, wantErr) || len(got) != 0 {
			t.Fatalf("Resolve = %v, %v; want no artists, %v", got, err, wantErr)
		}
	})
}
