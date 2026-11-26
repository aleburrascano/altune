package download

import fixture "altune/go-api/internal/download/internal/adapters/fakesources"

var (
	NewArtTrackSource = fixture.NewArtTrackSource
	NewUploadSource   = fixture.NewUploadSource
)

const ArtTrackURL = fixture.ArtTrackURL
