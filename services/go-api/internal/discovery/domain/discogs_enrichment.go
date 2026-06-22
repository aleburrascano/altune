package domain

// DiscogsEnrichment is the Discogs-derived detail-screen enrichment for one
// album (a Discogs "master"): the credits/personnel, styles, label + catalog,
// formats, companies, and community demand/rating that Discogs carries and
// MusicBrainz does not. Immutable value object — a live read surface fetched on
// detail-open, never persisted. Discogs is the credits/styles authority, so this
// complements MBEnrichment rather than duplicating it.
//
// Introduced by docs/providers/discogs.md (capabilities 3–6).
type DiscogsEnrichment struct {
	MasterID  int               // Discogs master id; 0 when unresolved
	Genres    []string          // top-level genres (coarse; MB owns the curated list)
	Styles    []string          // sub-genre layer below genre — the layer MB lacks
	Year      int               // master year; 0 when absent
	Credits   []DiscogsCredit   // album-wide personnel (producer / written-by / mixed-by / …)
	Labels    []DiscogsLabelRef // label + catalog number per edition
	Formats   []string          // e.g. "CD · Album", "Vinyl · LP"
	Country   string            // edition country
	Companies []DiscogsCompany  // recorded-at / mastered-at / copyright holders
	Community DiscogsCommunity  // have / want / rating
}

// DiscogsCredit is one personnel credit: a contributor and the role they played
// on the album (e.g. {Name: "Bēkon", Role: "Producer"}).
type DiscogsCredit struct {
	Name string
	Role string
}

// DiscogsLabelRef is a label and its catalog number for one edition.
type DiscogsLabelRef struct {
	Name  string
	Catno string
}

// DiscogsCompany is a company credit carrying the role it played (Discogs
// `entity_type_name`: "Recorded At", "Mastered At", "Copyright (c)", …).
type DiscogsCompany struct {
	Name string
	Role string
}

// DiscogsCommunity is the collector demand + community rating for an edition —
// a non-streaming popularity/quality signal (display/secondary only; never a
// rank driver, see docs/providers/discogs.md §6).
type DiscogsCommunity struct {
	Have   int
	Want   int
	Rating float64
	Votes  int
}

// EmptyDiscogsEnrichment returns a zero-value enrichment with non-nil
// collections, so the wire mapping never emits null lists. The graceful
// degradation path (unresolved master, Discogs error) returns this.
func EmptyDiscogsEnrichment() DiscogsEnrichment {
	return DiscogsEnrichment{
		Genres:    []string{},
		Styles:    []string{},
		Credits:   []DiscogsCredit{},
		Labels:    []DiscogsLabelRef{},
		Formats:   []string{},
		Companies: []DiscogsCompany{},
	}
}

// IsZero reports whether the enrichment carries no resolved album — used to
// decide there is nothing to render.
func (e DiscogsEnrichment) IsZero() bool {
	return e.MasterID == 0 &&
		len(e.Genres) == 0 &&
		len(e.Styles) == 0 &&
		e.Year == 0 &&
		len(e.Credits) == 0 &&
		len(e.Labels) == 0 &&
		len(e.Formats) == 0 &&
		e.Country == "" &&
		len(e.Companies) == 0 &&
		e.Community == DiscogsCommunity{}
}

// DiscogsArtistEnrichment is the Discogs-derived detail-screen enrichment for one
// artist: the biography, name history, group/member relationships, and external
// links Discogs carries (caps 7). Immutable value object — a live read surface
// fetched on detail-open, never persisted.
//
// Introduced by docs/providers/discogs.md (capability 7).
type DiscogsArtistEnrichment struct {
	ArtistID       int           // Discogs artist id; 0 when unresolved
	Profile        string        // biography, with Discogs markup stripped
	RealName       string        // legal name when distinct from the stage name
	Aliases        []string      // other names this entity records under
	NameVariations []string      // spelling/credit variants of the name
	Members        []string      // for a group: its member artists
	Groups         []string      // for a person: groups they belong to
	Links          []DiscogsLink // external links (official site, socials, wikis)
}

// DiscogsLink is one external link carrying a human label derived from its host
// (e.g. {Label: "Wikipedia", URL: "https://en.wikipedia.org/…"}).
type DiscogsLink struct {
	Label string
	URL   string
}

// EmptyDiscogsArtistEnrichment returns a zero-value artist enrichment with
// non-nil collections, so the wire mapping never emits null lists.
func EmptyDiscogsArtistEnrichment() DiscogsArtistEnrichment {
	return DiscogsArtistEnrichment{
		Aliases:        []string{},
		NameVariations: []string{},
		Members:        []string{},
		Groups:         []string{},
		Links:          []DiscogsLink{},
	}
}

// IsZero reports whether the artist enrichment carries nothing worth rendering.
func (e DiscogsArtistEnrichment) IsZero() bool {
	return e.ArtistID == 0 &&
		e.Profile == "" &&
		e.RealName == "" &&
		len(e.Aliases) == 0 &&
		len(e.NameVariations) == 0 &&
		len(e.Members) == 0 &&
		len(e.Groups) == 0 &&
		len(e.Links) == 0
}
