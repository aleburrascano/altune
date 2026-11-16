package discovery

import coverartarchive "altune/go-api/internal/discovery/internal/adapters/providers/coverartarchive"

var (
	NewCoverArtArchiveIdentityResolver = coverartarchive.NewCoverArtArchiveIdentityResolver
	NewCoverArtArchiveResolver         = coverartarchive.NewCoverArtArchiveResolver
)
