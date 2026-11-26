package download

import (
	recordingresolver "altune/go-api/internal/download/internal/core/musicbrainzmatch"
	"altune/go-api/internal/download/internal/core/ports"
)

type RecordingResolverPorts = recordingresolver.Ports

var (
	NewRecordingResolver  = recordingresolver.New
	NoopISRCLookup        = ports.NoopISRCLookup
	NoopRerecordingLookup = ports.NoopRerecordingLookup
)
