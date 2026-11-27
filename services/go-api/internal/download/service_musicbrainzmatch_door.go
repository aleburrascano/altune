package download

import (
	"altune/go-api/internal/download/internal/core/ports"
	recordingresolver "altune/go-api/internal/download/service/musicbrainzmatch"
)

type RecordingResolverPorts = recordingresolver.Ports

var (
	NewRecordingResolver  = recordingresolver.New
	NoopISRCLookup        = ports.NoopISRCLookup
	NoopRerecordingLookup = ports.NoopRerecordingLookup
)
