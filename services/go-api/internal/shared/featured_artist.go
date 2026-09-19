package shared

// FeaturedArtist is the plain value carried across the catalog<->discovery seam:
// discovery resolves credits, catalog persists them. It lives in the shared
// kernel so neither domain has to import the other to name the type they
// exchange. Each domain keeps its own richer FeaturedArtist for behavior; this
// carries only the four fields the bridge maps.
type FeaturedArtist struct {
	Name     string
	MBID     string
	DeezerID int64
	Role     string
}
