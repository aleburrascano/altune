package search

import coverartarchive "altune/go-api/internal/search/internal/adapters/providers/coverartarchive"

var (
	NewCoverArtArchiveIdentityResolver = coverartarchive.NewCoverArtArchiveIdentityResolver
	NewCoverArtArchiveResolver         = coverartarchive.NewCoverArtArchiveResolver
)

type CoverArtArchiveResolver = coverartarchive.CoverArtArchiveResolver
