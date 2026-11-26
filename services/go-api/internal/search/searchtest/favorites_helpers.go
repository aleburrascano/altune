package searchtest

import "altune/go-api/internal/search/internal/adapters/memory"

type FakeFavoritesRepo = memory.FakeFavoritesRepo

var (
	Subtitles  = memory.Subtitles
	FavoriteOf = memory.FavoriteOf
)
