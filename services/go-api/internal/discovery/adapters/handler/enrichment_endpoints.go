package handler

import (
	"log/slog"
	"net/http"
	"strings"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/httputil"
)

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
		httputil.WriteJSON(w, http.StatusOK, empty())
		return
	}
	result, err := call()
	if err != nil {
		slog.ErrorContext(r.Context(), logMsg, append([]any{"error", err}, logArgs...)...)
		httputil.InternalError(w)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, result)
}

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

	withEnricher(w, r, h.enrichSvc != nil,
		func() any { return enrichmentToDTO(domain.EmptyEnrichment()) },
		func() (any, error) {
			e, err := h.enrichSvc.Execute(r.Context(), kind, title, subtitle, mbid)
			if err != nil {
				return nil, err
			}
			return enrichmentToDTO(e), nil
		},
		"enrichment failed", "kind", kind.String(), "title", title)
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
	HasContent     bool              `json:"has_content"`
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
	title := strings.TrimSpace(r.URL.Query().Get("title"))
	subtitle := strings.TrimSpace(r.URL.Query().Get("subtitle"))
	if title == "" {
		httputil.BadRequest(w, "title is required")
		return
	}

	withEnricher(w, r, h.enrichers.LastFm != nil,
		func() any { return lastfmEnrichmentToDTO(domain.EmptyLastFmEnrichment()) },
		func() (any, error) {
			e, err := h.enrichers.LastFm.Execute(r.Context(), kind, title, subtitle)
			if err != nil {
				return nil, err
			}
			return lastfmEnrichmentToDTO(e), nil
		},
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
	title := strings.TrimSpace(r.URL.Query().Get("title"))
	subtitle := strings.TrimSpace(r.URL.Query().Get("subtitle"))
	if title == "" {
		httputil.BadRequest(w, "title is required")
		return
	}

	withEnricher(w, r, h.enrichers.Deezer != nil,
		func() any { return deezerEnrichmentToDTO(domain.EmptyDeezerEnrichment()) },
		func() (any, error) {
			e, err := h.enrichers.Deezer.Execute(r.Context(), kind, title, subtitle)
			if err != nil {
				return nil, err
			}
			return deezerEnrichmentToDTO(e), nil
		},
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
		HasContent:      e.HasRenderableContent(),
	}
}

func (h *DiscoveryHandler) handleLyrics(w http.ResponseWriter, r *http.Request) {
	title := strings.TrimSpace(r.URL.Query().Get("title"))
	subtitle := strings.TrimSpace(r.URL.Query().Get("subtitle"))
	if title == "" {
		httputil.BadRequest(w, "title is required")
		return
	}

	withEnricher(w, r, h.enrichers.Lyrics != nil,
		func() any { return lyricsToDTO(domain.EmptyDeezerLyrics()) },
		func() (any, error) {
			l, err := h.enrichers.Lyrics.Execute(r.Context(), title, subtitle)
			if err != nil {
				return nil, err
			}
			return lyricsToDTO(l), nil
		},
		"lyrics fetch failed", "title", title)
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
