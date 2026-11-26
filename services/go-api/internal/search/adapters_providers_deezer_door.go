package search

import deezer "altune/go-api/internal/search/internal/adapters/providers/deezer"

type DeezerAdapter = deezer.DeezerAdapter

var (
	NewDeezerAdapter       = deezer.NewDeezerAdapter
	NewDeezerLyricsAdapter = deezer.NewDeezerLyricsAdapter
)
