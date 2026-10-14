package main

import (
	"altune/go-api/internal/catalog/domain"
	"context"
	"slices"
)

const (
	maxArtistOptions  = 3
	minCatalogOverlap = 2
)

type providerArtist struct {
	ID   string
	Name string
}

type catalogView struct {
	Albums []string
	Titles []string
}

type artistSource interface {
	Name() source
	FindArtists(ctx context.Context, name string) ([]providerArtist, error)
	Catalog(ctx context.Context, artist providerArtist) (catalogView, error)
	Songs(ctx context.Context, artist providerArtist, view catalogView, t libraryTrack, keys domain.SearchKeys) ([]candidate, error)
}

type artistOption struct {
	providerArtist
	view catalogView
}

func (h holdings) overlap(view catalogView) int {
	albums, titles := keySet(view.Albums), keySet(view.Titles)
	hits := 0
	for _, held := range h {
		if (held.Album != "" && albums[held.Album]) || (held.Title != "" && titles[held.Title]) {
			hits++
		}
	}
	return hits
}

func keySet(names []string) map[string]bool {
	keys := make(map[string]bool, len(names))
	for _, name := range names {
		keys[catalogKey(name)] = true
	}
	return keys
}

func chooseArtist(owned holdings, storedName string, options []artistOption) (artistOption, bool) {
	overlapping := slices.DeleteFunc(slices.Clone(options), func(o artistOption) bool {
		return owned.overlap(o.view) < minCatalogOverlap
	})
	if len(overlapping) > 0 {
		return bestOverlap(owned, overlapping)
	}
	exact := slices.DeleteFunc(slices.Clone(options), func(o artistOption) bool {
		return artistKey(o.Name) != artistKey(storedName)
	})
	if len(exact) == 1 {
		return exact[0], true
	}
	return artistOption{}, false
}

func bestOverlap(owned holdings, overlapping []artistOption) (artistOption, bool) {
	slices.SortStableFunc(overlapping, func(a, b artistOption) int {
		return owned.overlap(b.view) - owned.overlap(a.view)
	})
	if len(overlapping) > 1 && owned.overlap(overlapping[0].view) == owned.overlap(overlapping[1].view) {
		return artistOption{}, false
	}
	return overlapping[0], true
}
