package acquisition

import recordingresolver "altune/go-api/internal/acquisition/internal/core/recordingresolver"

type RecordingResolver = recordingresolver.Resolver

var (
	NewRecordingResolver     = recordingresolver.New
	WithISRCAuthority        = recordingresolver.WithISRCAuthority
	WithRecordingLookup      = recordingresolver.WithRecordingLookup
	WithRerecordingAuthority = recordingresolver.WithRerecordingAuthority
)
