package acquisition

import discoverybridge "altune/go-api/internal/acquisition/internal/adapters/discoverybridge"

type RecordingResolver = discoverybridge.RecordingResolver

var (
	NewRecordingLookup       = discoverybridge.NewRecordingLookup
	NewRecordingResolver     = discoverybridge.NewRecordingResolver
	WithISRCAuthority        = discoverybridge.WithISRCAuthority
	WithRecordingLookup      = discoverybridge.WithRecordingLookup
	WithRerecordingAuthority = discoverybridge.WithRerecordingAuthority
)
