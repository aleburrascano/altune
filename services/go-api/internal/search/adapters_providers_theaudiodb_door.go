package search

import theaudiodb "altune/go-api/internal/search/internal/adapters/providers/theaudiodb"

var (
	NewTheAudioDBAdapter = theaudiodb.New
	WithTheAudioDBAPIKey = theaudiodb.WithAPIKey
)

type TheAudioDBAdapter = theaudiodb.Adapter
