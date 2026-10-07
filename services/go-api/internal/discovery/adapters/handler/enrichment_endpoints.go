package handler

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/service/enrich"
	"altune/go-api/internal/shared/httputil"
	"errors"
	"log/slog"
	"net/http"
	"strings"
)

const enrichmentUnavailableCode = "enrichment_unavailable"

func parseKindParam(w http.ResponseWriter, r *http.Request) (domain.ResultKind, bool) {
	kindStr := strings.TrimSpace(r.URL.Query().Get("kind"))
	if kindStr == "" {
		httputil.BadRequestCode(w, requestCodeInvalidParam, "kind is required")
		return 0, false
	}
	kind, err := domain.ParseResultKind(kindStr)
	if err != nil {
		httputil.BadRequestCode(w, requestCodeInvalidKind, "invalid kind")
		return 0, false
	}
	return kind, true
}

func withEnricher(
	w http.ResponseWriter,
	r *http.Request,
	available bool,
	empty func() any,
	call func() (any, error),
	logMsg string,
	logArgs ...any,
) {
	if !available {
		if empty == nil {
			httputil.WriteJSON(w, http.StatusServiceUnavailable, httputil.ErrorResponse{
				Detail: "enrichment is not configured",
				Code:   enrichmentUnavailableCode,
			})
			return
		}
		httputil.WriteJSON(w, http.StatusOK, empty())
		return
	}
	result, err := call()
	if err != nil {
		slog.ErrorContext(r.Context(), logMsg, append([]any{"error", err}, logArgs...)...)
		httputil.HandleServiceError(w, r, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, result)
}

func titleAndSubtitle(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	title, ok := textParam(w, r, "title")
	if !ok {
		return "", "", false
	}
	subtitle, ok := textParam(w, r, "subtitle")
	if !ok {
		return "", "", false
	}
	return title, subtitle, true
}

func splitDegraded(err error) (bool, error) {
	if errors.Is(err, enrich.ErrDegraded) {
		return true, nil
	}
	return false, err
}

// degradedCall replaces the four inline Execute/splitDegraded/stamp closures.
func degradedCall[E, D any](exec func() (E, error), toDTO func(E) D, stamp func(*D, bool)) func() (any, error) {
	return func() (any, error) {
		e, err := exec()
		degraded, err := splitDegraded(err)
		if err != nil {
			return nil, err
		}
		dto := toDTO(e)
		stamp(&dto, degraded)
		return dto, nil
	}
}

// requireTitle writes the 400 for an empty title and reports whether to continue.
func requireTitle(w http.ResponseWriter, title string) bool {
	if title == "" {
		httputil.BadRequestCode(w, requestCodeInvalidParam, "title is required")
		return false
	}
	return true
}

func (h *DiscoveryHandler) handleEnrichment(w http.ResponseWriter, r *http.Request) {
	kind, ok := parseKindParam(w, r)
	if !ok {
		return
	}
	title, subtitle, ok := titleAndSubtitle(w, r)
	if !ok {
		return
	}
	mbid, ok := mbidParam(w, r)
	if !ok {
		return
	}
	if title == "" && mbid == "" {
		httputil.BadRequestCode(w, requestCodeInvalidParam, "title or mbid is required")
		return
	}

	withEnricher(w, r, h.enrichSvc != nil,
		nil,
		degradedCall(
			func() (domain.MBEnrichment, error) {
				return h.enrichSvc.Execute(r.Context(), kind, title, subtitle, mbid)
			},
			enrichmentToDTO,
			func(d *EnrichmentResponseDTO, v bool) { d.Degraded = v }),
		"enrichment failed", "kind", kind.String(), "title", title)
}

type EnrichmentResponseDTO struct {
	MBID           string                        `json:"mbid"`
	Genres         []string                      `json:"genres"`
	Year           int                           `json:"year"`
	Rating         float64                       `json:"rating"`
	RatingVotes    int                           `json:"rating_votes"`
	PrimaryType    string                        `json:"primary_type"`
	SecondaryTypes []string                      `json:"secondary_types"`
	ExternalIDs    map[domain.ProviderKey]string `json:"external_ids"`
	ArtworkURL     string                        `json:"artwork_url"`
	HasContent     bool                          `json:"has_content"`
	Degraded       bool                          `json:"degraded"`
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
		ids = map[domain.ProviderKey]string{}
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
		HasContent:     e.HasRenderableContent(),
	}
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (h *DiscoveryHandler) handleLastFmEnrichment(w http.ResponseWriter, r *http.Request) {
	kind, ok := parseKindParam(w, r)
	if !ok {
		return
	}
	title, subtitle, ok := titleAndSubtitle(w, r)
	if !ok {
		return
	}
	if !requireTitle(w, title) {
		return
	}

	withEnricher(w, r, h.enrichers.LastFm != nil,
		func() any { return lastfmEnrichmentToDTO(domain.EmptyLastFmEnrichment()) },
		degradedCall(
			func() (domain.LastFmEnrichment, error) {
				return h.enrichers.LastFm.Execute(r.Context(), kind, title, subtitle)
			},
			lastfmEnrichmentToDTO,
			func(d *LastFmEnrichmentResponseDTO, v bool) { d.Degraded = v }),
		"lastfm enrichment failed", "kind", kind.String(), "title", title)
}

type LastFmEnrichmentResponseDTO struct {
	MBID       string   `json:"mbid"`
	Listeners  int64    `json:"listeners"`
	Playcount  int64    `json:"playcount"`
	Tags       []string `json:"tags"`
	Bio        string   `json:"bio"`
	Similar    []string `json:"similar"`
	Duration   int      `json:"duration"`
	Album      string   `json:"album"`
	HasContent bool     `json:"has_content"`
	Degraded   bool     `json:"degraded"`
}

func lastfmEnrichmentToDTO(e domain.LastFmEnrichment) LastFmEnrichmentResponseDTO {
	return LastFmEnrichmentResponseDTO{
		MBID:       e.MBID,
		Listeners:  e.Listeners,
		Playcount:  e.Playcount,
		Tags:       nonNilStrings(e.Tags),
		Bio:        e.Bio,
		Similar:    nonNilStrings(e.Similar),
		Duration:   e.Duration,
		Album:      e.Album,
		HasContent: e.HasRenderableContent(),
	}
}

func (h *DiscoveryHandler) handleDeezerEnrichment(w http.ResponseWriter, r *http.Request) {
	kind, ok := parseKindParam(w, r)
	if !ok {
		return
	}
	title, subtitle, ok := titleAndSubtitle(w, r)
	if !ok {
		return
	}
	if !requireTitle(w, title) {
		return
	}

	withEnricher(w, r, h.enrichers.Deezer != nil,
		func() any { return deezerEnrichmentToDTO(domain.EmptyDeezerEnrichment()) },
		degradedCall(
			func() (domain.DeezerEnrichment, error) {
				return h.enrichers.Deezer.Execute(r.Context(), kind, title, subtitle)
			},
			deezerEnrichmentToDTO,
			func(d *DeezerEnrichmentResponseDTO, v bool) { d.Degraded = v }),
		"deezer enrichment failed", "kind", kind.String(), "title", title)
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
	HasContent      bool             `json:"has_content"`
	Degraded        bool             `json:"degraded"`
}

func deezerEnrichmentToDTO(e domain.DeezerEnrichment) DeezerEnrichmentResponseDTO {
	return DeezerEnrichmentResponseDTO{
		BPM:             e.BPM,
		Gain:            e.Gain,
		Explicit:        e.Explicit,
		Label:           e.Label,
		Genres:          nonNilStrings(e.Genres),
		UPC:             e.UPC,
		RecordType:      string(e.RecordType),
		FeaturedArtists: domain.FeaturedArtistsToExtras(e.Featured),
		HasContent:      e.HasRenderableContent(),
	}
}

func (h *DiscoveryHandler) handleLyrics(w http.ResponseWriter, r *http.Request) {
	title, subtitle, ok := titleAndSubtitle(w, r)
	if !ok {
		return
	}
	if !requireTitle(w, title) {
		return
	}

	withEnricher(w, r, h.enrichers.Lyrics != nil,
		func() any { return lyricsToDTO(domain.EmptyDeezerLyrics()) },
		degradedCall(
			func() (domain.DeezerLyrics, error) { return h.enrichers.Lyrics.Execute(r.Context(), title, subtitle) },
			lyricsToDTO,
			func(d *LyricsResponseDTO, v bool) { d.Degraded = v }),
		"lyrics fetch failed", "title", title)
}

type LyricsResponseDTO struct {
	Plain       string          `json:"plain"`
	SyncedLines []SyncedLineDTO `json:"synced_lines"`
	Writers     []string        `json:"writers"`
	Copyright   string          `json:"copyright"`
	Degraded    bool            `json:"degraded"`
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
