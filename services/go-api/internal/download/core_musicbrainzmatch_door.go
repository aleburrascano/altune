package download

import (
	recordingresolver "altune/go-api/internal/download/internal/core/musicbrainzmatch"
	"altune/go-api/internal/download/internal/core/ports"
)

type (
	RecordingResolver      = recordingresolver.Resolver
	RecordingResolverPorts = recordingresolver.Ports
)

var (
	NewRecordingResolver     = recordingresolver.New
	MustNewRecordingResolver = recordingresolver.MustNew
	NoopRecordingSearch      = ports.NoopRecordingSearch
	NoopISRCLookup           = ports.NoopISRCLookup
	NoopRerecordingLookup    = ports.NoopRerecordingLookup
)

var ErrMissingRecordingResolverPort = recordingresolver.ErrMissingPort
