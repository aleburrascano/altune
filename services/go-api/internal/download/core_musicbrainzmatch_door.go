package download

import (
	recordingresolver "altune/go-api/internal/download/internal/core/musicbrainzmatch"
	"altune/go-api/internal/download/internal/core/ports"
)

type RecordingResolverPorts = recordingresolver.Ports

var (
	MustNewRecordingResolver = recordingresolver.MustNew
	NoopISRCLookup           = ports.NoopISRCLookup
	NoopRerecordingLookup    = ports.NoopRerecordingLookup
)
