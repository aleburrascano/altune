package discovery

import discogs "altune/go-api/internal/discovery/internal/adapters/providers/discogs"

type DiscogsAdapter = discogs.DiscogsAdapter

var NewDiscogsAdapter = discogs.NewDiscogsAdapter
