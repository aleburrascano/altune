package handler

import (
	"altune/go-api/internal/search/internal/domain"
	apierror "altune/go-api/internal/shared/apierror"
	"altune/go-api/internal/shared/httpserver"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

type Enricher interface {
	Execute(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid string) (domain.MusicBrainzEnrichment, error)
}

type UnconfiguredEnrichment struct{}

func (UnconfiguredEnrichment) Execute(context.Context, domain.ResultKind, string, string, string) (domain.MusicBrainzEnrichment, error) {
	return domain.MusicBrainzEnrichment{}, apierror.NewError(apierror.CodeEnrichmentUnavailable, "enrichment is not configured")
}

func parseKindParam(w http.ResponseWriter, r *http.Request) (domain.ResultKind, bool) {
	kindStr := strings.TrimSpace(r.URL.Query().Get("kind"))
	if kindStr == "" {
		httpserver.HandleServiceError(w, r, invalidParam("kind is required"))
		return 0, false
	}
	kind, err := domain.ParseResultKind(kindStr)
	if err != nil {
		httpserver.HandleServiceError(w, r, apierror.NewError(apierror.CodeDiscoveryInvalidKind, "invalid kind"))
		return 0, false
	}
	return kind, true
}

func withEnricher(
	w http.ResponseWriter,
	r *http.Request,
	call func(ctx context.Context) (any, error),
	operation string,
) {
	result, err := call(r.Context())
	if err != nil {
		httpserver.HandleServiceError(w, r, fmt.Errorf("%s: %w", operation, err))
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, result)
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
	if errors.Is(err, domain.ErrEnrichmentDegraded) {
		return true, nil
	}
	return false, err
}

func degradedCall[E, D any](exec func(context.Context) (E, error), toDTO func(E) D, stamp func(*D, bool)) func(context.Context) (any, error) {
	return func(ctx context.Context) (any, error) {
		e, err := exec(ctx)
		degraded, err := splitDegraded(err)
		if err != nil {
			return nil, err
		}
		dto := toDTO(e)
		stamp(&dto, degraded)
		return dto, nil
	}
}

func requireTitle(w http.ResponseWriter, r *http.Request, title string) bool {
	if title == "" {
		httpserver.HandleServiceError(w, r, invalidParam("title is required"))
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
		httpserver.HandleServiceError(w, r, invalidParam("title or mbid is required"))
		return
	}

	withEnricher(w, r,
		degradedCall(
			func(ctx context.Context) (domain.MusicBrainzEnrichment, error) {
				return h.enrichSvc.Execute(ctx, kind, title, subtitle, mbid)
			},
			enrichmentToDTO,
			func(d *EnrichmentResponseDTO, v bool) { d.Degraded = v }),
		fmt.Sprintf("enrich %s %q", kind, title))
}

func enrichmentToDTO(e domain.MusicBrainzEnrichment) EnrichmentResponseDTO {
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

func namedEnrichmentParams(w http.ResponseWriter, r *http.Request) (domain.ResultKind, string, string, bool) {
	kind, ok := parseKindParam(w, r)
	if !ok {
		return 0, "", "", false
	}
	title, subtitle, ok := titleAndSubtitle(w, r)
	if !ok || !requireTitle(w, r, title) {
		return 0, "", "", false
	}
	return kind, title, subtitle, true
}

func serveNamedEnrichment[E, D any](
	w http.ResponseWriter,
	r *http.Request,
	exec func(ctx context.Context, kind domain.ResultKind, title, subtitle string) (E, error),
	toDTO func(E) D,
	stamp func(*D, bool),
	source string,
) {
	kind, title, subtitle, ok := namedEnrichmentParams(w, r)
	if !ok {
		return
	}

	withEnricher(w, r,
		degradedCall(
			func(ctx context.Context) (E, error) { return exec(ctx, kind, title, subtitle) },
			toDTO, stamp),
		fmt.Sprintf("%s enrichment of %s %q", source, kind, title))
}

func (h *DiscoveryHandler) handleDeezerEnrichment(w http.ResponseWriter, r *http.Request) {
	serveNamedEnrichment(w, r, h.enrichers.Deezer.Execute, deezerEnrichmentToDTO,
		func(d *DeezerEnrichmentResponseDTO, v bool) { d.Degraded = v },
		"deezer")
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
	if !requireTitle(w, r, title) {
		return
	}

	withEnricher(w, r,
		degradedCall(
			func(ctx context.Context) (domain.DeezerLyrics, error) {
				return h.enrichers.Lyrics.Execute(ctx, title, subtitle)
			},
			lyricsToDTO,
			func(d *LyricsResponseDTO, v bool) { d.Degraded = v }),
		fmt.Sprintf("fetch lyrics for %q", title))
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
