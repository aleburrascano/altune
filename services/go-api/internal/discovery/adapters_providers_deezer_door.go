package discovery

import deezer "altune/go-api/internal/discovery/internal/adapters/providers/deezer"

type DeezerAdapter = deezer.DeezerAdapter

var (
	NewDeezerAdapter       = deezer.NewDeezerAdapter
	NewDeezerLyricsAdapter = deezer.NewDeezerLyricsAdapter
)
