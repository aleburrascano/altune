package acquisition

import (
	chromaprint "altune/go-api/internal/acquisition/internal/adapters/chromaprint"
	ports "altune/go-api/internal/acquisition/internal/core/ports"
)

type (
	ChromaprintIdentifier = chromaprint.Identifier
	ReleaseGroup          = ports.ReleaseGroup
)

var ErrChromaprintMissingAPIKey = chromaprint.ErrMissingAPIKey
