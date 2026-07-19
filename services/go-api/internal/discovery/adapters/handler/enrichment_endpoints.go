package handler

import (
	"log/slog"
	"net/http"
	"strings"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/httputil"
)

// Detail-open enrichment endpoints — one family per source (MusicBrainz,
// Discogs album/artist, Last.fm, Deezer, lyrics), each with its DTOs and
// mapper. These change only when a provider cap changes, never with the
// ranking pipeline.

// parseKindParam reads and validates the required "kind" query param, writing
// the 400 response itself on failure.
func parseKindParam(w http.ResponseWriter, r *http.Request) (domain.ResultKind, bool) {
	kindStr := strings.TrimSpace(r.URL.Query().Get("kind"))
	if kindStr == "" {
		httputil.BadRequest(w, "kind is required")
		return 0, false
	}
	kind, err := domain.ParseResultKind(kindStr)
	if err != nil {
		httputil.BadRequest(w, "invalid kind")
		return 0, false
	}
	return kind, true
}

// handleEnrichment serves MusicBrainz detail-open enrichment for one entity.
// Always 200 with the DTO (or an empty DTO) on the happy path — degradation is
// the service's concern; only request-shape problems are 4xx.
func (h *DiscoveryHandler) handleEnrichment(w http.ResponseWriter, r *http.Request) {
	kind, ok := parseKindParam(w, r)
	if !ok {
		return
	}
	title := strings.TrimSpace(r.URL.Query().Get("title"))
	subtitle := strings.TrimSpace(r.URL.Query().Get("subtitle"))
	mbid := strings.TrimSpace(r.URL.Query().Get("mbid"))
	if title == "" && mbid == "" {
		httputil.BadRequest(w, "title or mbid is required")
		return
	}

	if h.enrichSvc == nil {
		httputil.WriteJSON(w, http.StatusOK, enrichmentToDTO(domain.EmptyEnrichment()))
		return
	}

	e, err := h.enrichSvc.Execute(r.Context(), kind, title, subtitle, mbid)
	if err != nil {
		slog.ErrorContext(r.Context(), "enrichment failed",
			"error", err, "kind", kind.String(), "title", title)
		httputil.InternalError(w)
		return
	}

	httputil.WriteJSON(w, http.StatusOK, enrichmentToDTO(e))
}

type EnrichmentResponseDTO struct {
	MBID           string            `json:"mbid"`
	Genres         []string          `json:"genres"`
	Year           int               `json:"year"`
	Rating         float64           `json:"rating"`
	RatingVotes    int               `json:"rating_votes"`
	PrimaryType    string            `json:"primary_type"`
	SecondaryTypes []string          `json:"secondary_types"`
	ExternalIDs    map[string]string `json:"external_ids"`
	ArtworkURL     string            `json:"artwork_url"`
}

func enrichmentToDTO(e domain.MBEnrichment) EnrichmentResponseDTO {
	genres := e.Genres
	if genres == nil {
		genres = []string{}
	}
	secondary := e.SecondaryTypes
	if secondary == nil {
		secondary = []string{}
	}
	ids := e.ExternalIDs
	if ids == nil {
		ids = map[string]string{}
	}
	return EnrichmentResponseDTO{
		MBID:           e.MBID,
		Genres:         genres,
		Year:           e.Year,
		Rating:         e.Rating,
		RatingVotes:    e.RatingVotes,
		PrimaryType:    e.PrimaryType,
		SecondaryTypes: secondary,
		ExternalIDs:    ids,
		ArtworkURL:     e.ArtworkURL,
	}
}

// handleDiscogsEnrichment serves Discogs detail-open album enrichment: credits,
// styles, label/catalog, companies, and community signal (docs/providers/discogs.md
// caps 3–6). Album-scoped — resolved from `album` + `artist`. Always 200 with the
// DTO (or an empty DTO); only request-shape problems are 4xx.
func (h *DiscoveryHandler) handleDiscogsEnrichment(w http.ResponseWriter, r *http.Request) {
	album := strings.TrimSpace(r.URL.Query().Get("album"))
	artist := strings.TrimSpace(r.URL.Query().Get("artist"))
	if album == "" {
		httputil.BadRequest(w, "album is required")
		return
	}

	if h.enrichers.Discogs == nil {
		httputil.WriteJSON(w, http.StatusOK, discogsEnrichmentToDTO(domain.EmptyDiscogsEnrichment()))
		return
	}

	e, err := h.enrichers.Discogs.Execute(r.Context(), artist, album)
	if err != nil {
		slog.ErrorContext(r.Context(), "discogs enrichment failed",
			"error", err, "album", album, "artist", artist)
		httputil.InternalError(w)
		return
	}

	httputil.WriteJSON(w, http.StatusOK, discogsEnrichmentToDTO(e))
}

type DiscogsCreditDTO struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

type DiscogsLabelDTO struct {
	Name  string `json:"name"`
	Catno string `json:"catno"`
}

type DiscogsCompanyDTO struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

type DiscogsCommunityDTO struct {
	Have   int     `json:"have"`
	Want   int     `json:"want"`
	Rating float64 `json:"rating"`
	Votes  int     `json:"votes"`
}

type DiscogsEnrichmentResponseDTO struct {
	MasterID  int                 `json:"master_id"`
	Genres    []string            `json:"genres"`
	Styles    []string            `json:"styles"`
	Year      int                 `json:"year"`
	Credits   []DiscogsCreditDTO  `json:"credits"`
	Labels    []DiscogsLabelDTO   `json:"labels"`
	Formats   []string            `json:"formats"`
	Country   string              `json:"country"`
	Companies []DiscogsCompanyDTO `json:"companies"`
	Community DiscogsCommunityDTO `json:"community"`
}

func discogsEnrichmentToDTO(e domain.DiscogsEnrichment) DiscogsEnrichmentResponseDTO {
	credits := make([]DiscogsCreditDTO, len(e.Credits))
	for i, c := range e.Credits {
		credits[i] = DiscogsCreditDTO{Name: c.Name, Role: c.Role}
	}
	labels := make([]DiscogsLabelDTO, len(e.Labels))
	for i, l := range e.Labels {
		labels[i] = DiscogsLabelDTO{Name: l.Name, Catno: l.Catno}
	}
	companies := make([]DiscogsCompanyDTO, len(e.Companies))
	for i, c := range e.Companies {
		companies[i] = DiscogsCompanyDTO{Name: c.Name, Role: c.Role}
	}
	genres := e.Genres
	if genres == nil {
		genres = []string{}
	}
	styles := e.Styles
	if styles == nil {
		styles = []string{}
	}
	formats := e.Formats
	if formats == nil {
		formats = []string{}
	}
	return DiscogsEnrichmentResponseDTO{
		MasterID:  e.MasterID,
		Genres:    genres,
		Styles:    styles,
		Year:      e.Year,
		Credits:   credits,
		Labels:    labels,
		Formats:   formats,
		Country:   e.Country,
		Companies: companies,
		Community: DiscogsCommunityDTO{
			Have:   e.Community.Have,
			Want:   e.Community.Want,
			Rating: e.Community.Rating,
			Votes:  e.Community.Votes,
		},
	}
}

// handleDiscogsArtistEnrichment serves Discogs detail-open artist enrichment:
// bio, name history, group/member links, and external links (cap 7). Resolved
// from `name`. Always 200 with the DTO (or an empty DTO); only request-shape
// problems are 4xx.
func (h *DiscoveryHandler) handleDiscogsArtistEnrichment(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		httputil.BadRequest(w, "name is required")
		return
	}

	if h.enrichers.DiscogsArtist == nil {
		httputil.WriteJSON(w, http.StatusOK, discogsArtistEnrichmentToDTO(domain.EmptyDiscogsArtistEnrichment()))
		return
	}

	e, err := h.enrichers.DiscogsArtist.Execute(r.Context(), name)
	if err != nil {
		slog.ErrorContext(r.Context(), "discogs artist enrichment failed",
			"error", err, "name", name)
		httputil.InternalError(w)
		return
	}

	httputil.WriteJSON(w, http.StatusOK, discogsArtistEnrichmentToDTO(e))
}

type DiscogsLinkDTO struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

type DiscogsArtistEnrichmentResponseDTO struct {
	ArtistID       int              `json:"artist_id"`
	Profile        string           `json:"profile"`
	RealName       string           `json:"real_name"`
	Aliases        []string         `json:"aliases"`
	NameVariations []string         `json:"name_variations"`
	Members        []string         `json:"members"`
	Groups         []string         `json:"groups"`
	Links          []DiscogsLinkDTO `json:"links"`
}

func discogsArtistEnrichmentToDTO(e domain.DiscogsArtistEnrichment) DiscogsArtistEnrichmentResponseDTO {
	links := make([]DiscogsLinkDTO, len(e.Links))
	for i, l := range e.Links {
		links[i] = DiscogsLinkDTO{Label: l.Label, URL: l.URL}
	}
	return DiscogsArtistEnrichmentResponseDTO{
		ArtistID:       e.ArtistID,
		Profile:        e.Profile,
		RealName:       e.RealName,
		Aliases:        nonNilStrings(e.Aliases),
		NameVariations: nonNilStrings(e.NameVariations),
		Members:        nonNilStrings(e.Members),
		Groups:         nonNilStrings(e.Groups),
		Links:          links,
	}
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// handleLastFmEnrichment serves Last.fm detail-open enrichment for one entity:
// listen-based popularity, weighted tags, bio, and (for artists) similar
// artists (docs/providers/lastfm.md cap 3). Kind-dispatched from `kind` +
// `title` + `subtitle`. Always 200 with the DTO (or an empty DTO); only
// request-shape problems are 4xx.
func (h *DiscoveryHandler) handleLastFmEnrichment(w http.ResponseWriter, r *http.Request) {
	kind, ok := parseKindParam(w, r)
	if !ok {
		return
	}
	title := strings.TrimSpace(r.URL.Query().Get("title"))
	subtitle := strings.TrimSpace(r.URL.Query().Get("subtitle"))
	if title == "" {
		httputil.BadRequest(w, "title is required")
		return
	}

	if h.enrichers.LastFm == nil {
		httputil.WriteJSON(w, http.StatusOK, lastfmEnrichmentToDTO(domain.EmptyLastFmEnrichment()))
		return
	}

	e, err := h.enrichers.LastFm.Execute(r.Context(), kind, title, subtitle)
	if err != nil {
		slog.ErrorContext(r.Context(), "lastfm enrichment failed",
			"error", err, "kind", kind.String(), "title", title)
		httputil.InternalError(w)
		return
	}

	httputil.WriteJSON(w, http.StatusOK, lastfmEnrichmentToDTO(e))
}

type LastFmEnrichmentResponseDTO struct {
	MBID      string   `json:"mbid"`
	Listeners int64    `json:"listeners"`
	Playcount int64    `json:"playcount"`
	Tags      []string `json:"tags"`
	Bio       string   `json:"bio"`
	Similar   []string `json:"similar"`
	Duration  int      `json:"duration"`
	Album     string   `json:"album"`
}

func lastfmEnrichmentToDTO(e domain.LastFmEnrichment) LastFmEnrichmentResponseDTO {
	return LastFmEnrichmentResponseDTO{
		MBID:      e.MBID,
		Listeners: e.Listeners,
		Playcount: e.Playcount,
		Tags:      nonNilStrings(e.Tags),
		Bio:       e.Bio,
		Similar:   nonNilStrings(e.Similar),
		Duration:  e.Duration,
		Album:     e.Album,
	}
}

// handleDeezerEnrichment serves Deezer detail-open enrichment for one track or
// album: the audio fields (bpm/gain) + explicit flag for tracks, and label /
// genres / barcode / record type for albums (docs/providers/deezer.md caps 7–8).
// Kind-dispatched from `kind` + `title` + `subtitle`. Always 200 with the DTO
// (or an empty DTO); only request-shape problems are 4xx.
func (h *DiscoveryHandler) handleDeezerEnrichment(w http.ResponseWriter, r *http.Request) {
	kind, ok := parseKindParam(w, r)
	if !ok {
		return
	}
	title := strings.TrimSpace(r.URL.Query().Get("title"))
	subtitle := strings.TrimSpace(r.URL.Query().Get("subtitle"))
	if title == "" {
		httputil.BadRequest(w, "title is required")
		return
	}

	if h.enrichers.Deezer == nil {
		httputil.WriteJSON(w, http.StatusOK, deezerEnrichmentToDTO(domain.EmptyDeezerEnrichment()))
		return
	}

	e, err := h.enrichers.Deezer.Execute(r.Context(), kind, title, subtitle)
	if err != nil {
		slog.ErrorContext(r.Context(), "deezer enrichment failed",
			"error", err, "kind", kind.String(), "title", title)
		httputil.InternalError(w)
		return
	}

	httputil.WriteJSON(w, http.StatusOK, deezerEnrichmentToDTO(e))
}

type DeezerEnrichmentResponseDTO struct {
	BPM             int              `json:"bpm"`
	Gain            float64          `json:"gain"`
	Explicit        bool             `json:"explicit"`
	Label           string           `json:"label"`
	Genres          []string         `json:"genres"`
	UPC             string           `json:"upc"`
	RecordType      string           `json:"record_type"`
	FeaturedArtists []map[string]any `json:"featured_artists,omitempty"`
}

func deezerEnrichmentToDTO(e domain.DeezerEnrichment) DeezerEnrichmentResponseDTO {
	return DeezerEnrichmentResponseDTO{
		BPM:             e.BPM,
		Gain:            e.Gain,
		Explicit:        e.Explicit,
		Label:           e.Label,
		Genres:          nonNilStrings(e.Genres),
		UPC:             e.UPC,
		RecordType:      e.RecordType,
		FeaturedArtists: domain.FeaturedArtistsToExtras(e.Featured),
	}
}

// handleLyrics serves Deezer lyrics for one track: the full plain text, the
// time-synced lines (when available), the songwriter credits, and the copyright
// line (docs/providers/deezer.md cap 6). Identified by `title` (track) +
// `subtitle` (artist). Always 200 with the DTO (or an empty DTO); only
// request-shape problems are 4xx. Lyrics apply to tracks only — no kind param.
func (h *DiscoveryHandler) handleLyrics(w http.ResponseWriter, r *http.Request) {
	title := strings.TrimSpace(r.URL.Query().Get("title"))
	subtitle := strings.TrimSpace(r.URL.Query().Get("subtitle"))
	if title == "" {
		httputil.BadRequest(w, "title is required")
		return
	}

	if h.enrichers.Lyrics == nil {
		httputil.WriteJSON(w, http.StatusOK, lyricsToDTO(domain.EmptyDeezerLyrics()))
		return
	}

	l, err := h.enrichers.Lyrics.Execute(r.Context(), title, subtitle)
	if err != nil {
		slog.ErrorContext(r.Context(), "lyrics fetch failed", "error", err, "title", title)
		httputil.InternalError(w)
		return
	}

	httputil.WriteJSON(w, http.StatusOK, lyricsToDTO(l))
}

type LyricsResponseDTO struct {
	Plain       string          `json:"plain"`
	SyncedLines []SyncedLineDTO `json:"synced_lines"`
	Writers     []string        `json:"writers"`
	Copyright   string          `json:"copyright"`
}

type SyncedLineDTO struct {
	Timecode     string `json:"timecode"`
	Line         string `json:"line"`
	Milliseconds int64  `json:"milliseconds"`
	Duration     int64  `json:"duration"`
}

func lyricsToDTO(l domain.DeezerLyrics) LyricsResponseDTO {
	lines := make([]SyncedLineDTO, len(l.SyncedLines))
	for i, ln := range l.SyncedLines {
		lines[i] = SyncedLineDTO{
			Timecode:     ln.Timecode,
			Line:         ln.Line,
			Milliseconds: ln.Milliseconds,
			Duration:     ln.Duration,
		}
	}
	return LyricsResponseDTO{
		Plain:       l.Plain,
		SyncedLines: lines,
		Writers:     nonNilStrings(l.Writers),
		Copyright:   l.Copyright,
	}
}
