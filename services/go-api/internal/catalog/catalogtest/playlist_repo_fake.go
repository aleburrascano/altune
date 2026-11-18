package catalogtest

import memory "altune/go-api/internal/catalog/internal/adapters/memory"

type PlaylistRepo = memory.PlaylistRepo

var NewPlaylistRepo = memory.NewPlaylistRepo
