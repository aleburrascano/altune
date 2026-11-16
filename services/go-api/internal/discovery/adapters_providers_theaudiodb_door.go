package discovery

import theaudiodb "altune/go-api/internal/discovery/internal/adapters/providers/theaudiodb"

var (
	NewTheAudioDBAdapter = theaudiodb.New
	WithTheAudioDBAPIKey = theaudiodb.WithAPIKey
)

type TheAudioDBAdapter = theaudiodb.Adapter
