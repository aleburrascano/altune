package librarytest

import (
	"altune/go-api/internal/library/internal/adapters/memory"
)

type PlaylistRepo = memory.PlaylistRepo

var NewPlaylistRepo = memory.NewPlaylistRepo
