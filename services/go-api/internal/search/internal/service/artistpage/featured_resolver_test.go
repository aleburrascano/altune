package artistpage_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/service/artistpage"
	"altune/go-api/internal/search/searchtest"
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeMBFeat struct {
	results []search.SearchResult
	err     error
}

func (f fakeMBFeat) SearchStructured(_ context.Context, _, _ string, _ map[search.ResultKind]bool) ([]search.SearchResult, error) {
	return f.results, f.err
}

type fakeDeezerFeat struct {
	id        string
	feats     []search.FeaturedArtist
	err       error
	lookupErr error
}

func (f fakeDeezerFeat) ResolveID(_ context.Context, _ search.ResultKind, _, _ string) (string, error) {
	return f.id, f.err
}

func (f fakeDeezerFeat) LookupTrackFeatured(_ context.Context, _ string) ([]search.FeaturedArtist, error) {
	return f.feats, f.lookupErr
}

func mbResultWithFeatured(feats ...search.FeaturedArtist) search.SearchResult {
	extras := make([]map[string]any, 0, len(feats))
	for _, f := range feats {
		extras = append(extras, f.ToExtrasMap())
	}
	return memory.NewResult(
		search.ResultKindTrack,
		"Song",
		memory.WithExtras(map[string]any{"featured_artists": extras}),
	)
}

func resolveFeatured(t *testing.T, mb, dz []search.FeaturedArtist) []search.FeaturedArtist {
	t.Helper()
	got, err := searchtest.NewFeaturedResolver(t, fakeMBFeat{results: []search.SearchResult{mbResultWithFeatured(mb...)}}, fakeDeezerFeat{id: "42", feats: dz}).Resolve(context.Background(), "Artist", "Song")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return got
}

func TestFeaturedArtistResolver_Resolve(t *testing.T) {
	ctx := context.Background()

	t.Run("merges mb + deezer, mb-primary", func(t *testing.T) {
		mb := fakeMBFeat{results: []search.SearchResult{mbResultWithFeatured(
			search.FeaturedArtist{Name: "SZA", MBID: "m1", Role: "featured"},
		)}}
		dz := fakeDeezerFeat{id: "42", feats: []search.FeaturedArtist{
			{Name: "SZA", DeezerID: 7, Role: "featured"},
			{Name: "Doja", DeezerID: 8, Role: "featured"},
		}}
		got, _ := searchtest.NewFeaturedResolver(t, mb, dz).Resolve(ctx, "Artist", "Song")
		if len(got) != 2 {
			t.Fatalf("expected 2, got %d (%+v)", len(got), got)
		}
		if got[0].MBID != "m1" || got[0].DeezerID != 7 {
			t.Errorf("merged[0] = %+v, want MBID m1 + DeezerID 7", got[0])
		}
		if got[1].Name != "Doja" {
			t.Errorf("gap-fill = %+v, want Doja", got[1])
		}
	})

	t.Run("mb error degrades to deezer only", func(t *testing.T) {
		mb := fakeMBFeat{err: errors.New("mb down")}
		dz := fakeDeezerFeat{id: "1", feats: []search.FeaturedArtist{{Name: "Guest", DeezerID: 9}}}
		got, _ := searchtest.NewFeaturedResolver(t, mb, dz).Resolve(ctx, "A", "B")
		if len(got) != 1 || got[0].Name != "Guest" {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("deezer unresolved degrades to mb only", func(t *testing.T) {
		mb := fakeMBFeat{results: []search.SearchResult{mbResultWithFeatured(
			search.FeaturedArtist{Name: "Only MB", MBID: "m"},
		)}}
		dz := fakeDeezerFeat{id: ""}
		got, _ := searchtest.NewFeaturedResolver(t, mb, dz).Resolve(ctx, "A", "B")
		if len(got) != 1 || got[0].Name != "Only MB" {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("neither yields empty", func(t *testing.T) {
		got, _ := searchtest.NewFeaturedResolver(t, fakeMBFeat{}, fakeDeezerFeat{}).Resolve(ctx, "A", "B")
		if len(got) != 0 {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("both sources erroring returns an error", func(t *testing.T) {
		mb := fakeMBFeat{err: errors.New("mb down")}
		dz := fakeDeezerFeat{err: errors.New("dz down")}
		got, err := searchtest.NewFeaturedResolver(t, mb, dz).Resolve(ctx, "A", "B")
		if err == nil || len(got) != 0 {
			t.Fatalf("got %+v, err %v; want error", got, err)
		}
	})

	t.Run("deezer lookup error counts as failure", func(t *testing.T) {
		mb := fakeMBFeat{err: errors.New("mb down")}
		dz := fakeDeezerFeat{id: "1", lookupErr: errors.New("lookup down")}
		if _, err := searchtest.NewFeaturedResolver(t, mb, dz).Resolve(ctx, "A", "B"); err == nil {
			t.Fatal("want error")
		}
	})

	t.Run("one errors and other has none is a genuine none", func(t *testing.T) {
		mb := fakeMBFeat{err: errors.New("mb down")}
		got, err := searchtest.NewFeaturedResolver(t, mb, fakeDeezerFeat{}).Resolve(ctx, "A", "B")
		if err != nil || got != nil {
			t.Fatalf("got %+v, err %v; want nil, nil", got, err)
		}
	})

	t.Run("cancelled ctx is a failure not none", func(t *testing.T) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		_, err := searchtest.NewFeaturedResolver(t, fakeMBFeat{}, fakeDeezerFeat{}).Resolve(cctx, "A", "B")
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	})
}

func TestNewFeaturedArtistResolver_RefusesAMissingSource(t *testing.T) {
	cases := map[string]struct {
		mb     artistpage.MBFeaturedSearcher
		deezer artistpage.DeezerFeaturedLookup
		want   string
	}{
		"musicbrainz": {mb: nil, deezer: fakeDeezerFeat{}, want: "MusicBrainz featured searcher"},
		"deezer":      {mb: fakeMBFeat{}, deezer: nil, want: "Deezer featured lookup"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			resolver, err := artistpage.NewFeaturedArtistResolver(tc.mb, tc.deezer)
			if !errors.Is(err, artistpage.ErrCollaboratorRequired) || resolver != nil {
				t.Fatalf("resolver = %v, err = %v, want artistpage.ErrCollaboratorRequired", resolver, err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %q, want it to name %q", err, tc.want)
			}
		})
	}
}

func TestFeaturedArtistResolver_ASwitchedOffSourceIsNotAnAttempt(t *testing.T) {
	ctx := context.Background()
	cancelled, cancel := context.WithCancel(ctx)
	cancel()

	t.Run("both switched off is none even when cancelled", func(t *testing.T) {
		got, err := searchtest.NewFeaturedResolver(t, search.NoMBFeatured{}, artistpage.NoDeezerFeatured{}).Resolve(cancelled, "A", "B")
		if err != nil || got != nil {
			t.Fatalf("got %+v, err %v; want nil, nil", got, err)
		}
	})

	t.Run("switched off musicbrainz leaves deezer's credits", func(t *testing.T) {
		dz := fakeDeezerFeat{id: "1", feats: []search.FeaturedArtist{{Name: "SZA", DeezerID: 7}}}
		got, err := searchtest.NewFeaturedResolver(t, search.NoMBFeatured{}, dz).Resolve(ctx, "A", "B")
		if err != nil || len(got) != 1 || got[0].DeezerID != 7 {
			t.Fatalf("got %+v, err %v; want deezer's one credit", got, err)
		}
	})

	t.Run("switched off musicbrainz and a failing deezer is all sources failed", func(t *testing.T) {
		dz := fakeDeezerFeat{err: errors.New("dz down")}
		got, err := searchtest.NewFeaturedResolver(t, search.NoMBFeatured{}, dz).Resolve(ctx, "A", "B")
		if err == nil || got != nil {
			t.Fatalf("got %+v, err %v; want the all-sources-failed error", got, err)
		}
	})

	t.Run("switched off deezer and an empty musicbrainz is none", func(t *testing.T) {
		got, err := searchtest.NewFeaturedResolver(t, fakeMBFeat{}, artistpage.NoDeezerFeatured{}).Resolve(ctx, "A", "B")
		if err != nil || got != nil {
			t.Fatalf("got %+v, err %v; want nil, nil", got, err)
		}
	})

	t.Run("switched off deezer with a cancelled context is a failure", func(t *testing.T) {
		_, err := searchtest.NewFeaturedResolver(t, fakeMBFeat{}, artistpage.NoDeezerFeatured{}).Resolve(cancelled, "A", "B")
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	})
}

func TestFeaturedArtistResolver_MergesMusicBrainzAndDeezerCredits(t *testing.T) {
	t.Run("mb only", func(t *testing.T) {
		mb := []search.FeaturedArtist{{Name: "SZA", MBID: "m1"}}
		got := resolveFeatured(t, mb, nil)
		if len(got) != 1 || got[0].MBID != "m1" {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("deezer only", func(t *testing.T) {
		dz := []search.FeaturedArtist{{Name: "SZA", DeezerID: 7}}
		got := resolveFeatured(t, nil, dz)
		if len(got) != 1 || got[0].DeezerID != 7 {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("both agree — same name merges ids, MB order kept", func(t *testing.T) {
		mb := []search.FeaturedArtist{{Name: "SZA", MBID: "m1"}}
		dz := []search.FeaturedArtist{{Name: "sza", DeezerID: 7}}
		got := resolveFeatured(t, mb, dz)
		if len(got) != 1 {
			t.Fatalf("expected 1 merged, got %d (%+v)", len(got), got)
		}
		if got[0].MBID != "m1" || got[0].DeezerID != 7 {
			t.Errorf("merged entry = %+v, want MBID m1 + DeezerID 7", got[0])
		}
	})

	t.Run("deezer fills gap MB missed", func(t *testing.T) {
		mb := []search.FeaturedArtist{{Name: "SZA", MBID: "m1"}}
		dz := []search.FeaturedArtist{{Name: "SZA", DeezerID: 7}, {Name: "Doja", DeezerID: 8}}
		got := resolveFeatured(t, mb, dz)
		if len(got) != 2 {
			t.Fatalf("expected 2, got %d (%+v)", len(got), got)
		}
		if got[1].Name != "Doja" || got[1].DeezerID != 8 {
			t.Errorf("gap-fill entry = %+v, want Doja / 8", got[1])
		}
	})

	t.Run("neither", func(t *testing.T) {
		if got := resolveFeatured(t, nil, nil); len(got) != 0 {
			t.Errorf("expected empty, got %+v", got)
		}
	})
}
