package download

import (
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/download/internal/service"
)

type RecordingResolverPorts = service.ResolverPorts

var (
	NewRecordingResolver  = service.NewMusicBrainzResolver
	NoopISRCLookup        = ports.NoopISRCLookup
	NoopRerecordingLookup = ports.NoopRerecordingLookup
)
