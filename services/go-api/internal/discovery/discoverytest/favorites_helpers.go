package discoverytest

import "altune/go-api/internal/discovery/internal/adapters/memory"

type FakeFavoritesRepo = memory.FakeFavoritesRepo

var (
	Subtitles  = memory.Subtitles
	FavoriteOf = memory.FavoriteOf
)
