package download

import recordingresolver "altune/go-api/internal/download/internal/core/musicbrainzmatch"

type RecordingResolver = recordingresolver.Resolver

var (
	NewRecordingResolver  = recordingresolver.New
	WithISRCLookup        = recordingresolver.WithISRCLookup
	WithRecordingLookup   = recordingresolver.WithRecordingLookup
	WithRerecordingLookup = recordingresolver.WithRerecordingLookup
)
