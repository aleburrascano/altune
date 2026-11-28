package service

import (
	"altune/go-api/internal/search/internal/domain"
	"context"
)

type NoMBFeatured struct{}

func (NoMBFeatured) SearchStructured(context.Context, string, string, map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	return nil, errFeaturedSourceAbsent
}

type NoDeezerFeatured struct{}

func (NoDeezerFeatured) ResolveID(context.Context, domain.ResultKind, string, string) (string, error) {
	return "", errFeaturedSourceAbsent
}

func (NoDeezerFeatured) LookupTrackFeatured(context.Context, string) ([]domain.FeaturedArtist, error) {
	return nil, errFeaturedSourceAbsent
}
