package discovery

import catalogbridge "altune/go-api/internal/discovery/internal/adapters/catalogbridge"

var (
	NewOwnershipReader   = catalogbridge.NewOwnershipReader
	NewTrackNumberWriter = catalogbridge.NewTrackNumberWriter
)
