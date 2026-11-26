package search

import discogs "altune/go-api/internal/search/internal/adapters/providers/discogs"

type DiscogsAdapter = discogs.DiscogsAdapter

var NewDiscogsAdapter = discogs.NewDiscogsAdapter
