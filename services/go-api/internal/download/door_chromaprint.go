package download

import (
	chromaprint "altune/go-api/internal/download/internal/adapters/chromaprint"
	ports "altune/go-api/internal/download/internal/core/ports"
)

type (
	ChromaprintIdentifier = chromaprint.Identifier
	ReleaseGroup          = ports.ReleaseGroup
)

var ErrChromaprintMissingAPIKey = chromaprint.ErrMissingAPIKey
