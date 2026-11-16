package acquisition

import fixture "altune/go-api/internal/acquisition/internal/adapters/fixture"

var (
	NewArtTrackSource = fixture.NewArtTrackSource
	NewUploadSource   = fixture.NewUploadSource
)

const (
	ArtTrackURL       = fixture.ArtTrackURL
	FixtureSourceName = fixture.SourceName
	UploadSourceName  = fixture.UploadSourceName
)
