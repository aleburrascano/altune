package providers

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"
)

type MusicBrainzAdapter struct {
	client    *http.Client
	userAgent string
	limiter   *minIntervalLimiter

	identityMemo *mbMemo[*ports.ArtistIdentity]
	releaseMemo  *mbMemo[[]mbReleaseGroup]
	releaseSF    singleflight.Group
}

func NewMusicBrainzAdapter(client *http.Client, userAgent string) *MusicBrainzAdapter {
	return &MusicBrainzAdapter{
		client:       client,
		userAgent:    userAgent,
		limiter:      newMinIntervalLimiter(time.Second),
		identityMemo: newMBMemo[*ports.ArtistIdentity](mbMemoTTL),
		releaseMemo:  newMBMemo[[]mbReleaseGroup](mbMemoTTL),
	}
}

func (a *MusicBrainzAdapter) Name() domain.ProviderName { return domain.ProviderMusicBrainz }

func (a *MusicBrainzAdapter) SupportedKinds() map[domain.ResultKind]bool {
	return allSearchKinds()
}

func (a *MusicBrainzAdapter) SearchTimeout() time.Duration {
	return 5 * time.Second
}

func (a *MusicBrainzAdapter) Search(ctx context.Context, query string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	return searchAcrossKinds(ctx, a.Name().String(), query, kinds, a.SupportedKinds(),
		func(ctx context.Context, kind domain.ResultKind) ([]domain.SearchResult, error) {
			return a.searchKind(ctx, query, kind)
		})
}

func (a *MusicBrainzAdapter) SearchStructured(ctx context.Context, artist, track string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	var results []domain.SearchResult
	for kind := range kinds {
		if !a.SupportedKinds()[kind] {
			continue
		}
		q := mbStructuredQuery(artist, track, kind)
		items, err := a.searchKind(ctx, q, kind)
		if err != nil {
			continue
		}
		results = append(results, items...)
	}
	return results, nil
}

func mbEscapeQuotes(s string) string {
	return strings.ReplaceAll(s, `"`, `\"`)
}

func mbStructuredQuery(artist, track string, kind domain.ResultKind) string {
	switch kind {
	case domain.ResultKindTrack:
		//nolint:gocritic // Lucene query DSL: quotes are pre-escaped by mbEscapeQuotes; %q would double-escape and corrupt the query
		return fmt.Sprintf(`artist:"%s" AND recording:"%s"`, mbEscapeQuotes(artist), mbEscapeQuotes(track))
	case domain.ResultKindAlbum:
		//nolint:gocritic // Lucene query DSL: quotes are pre-escaped by mbEscapeQuotes; %q would double-escape and corrupt the query
		return fmt.Sprintf(`artist:"%s" AND release:"%s"`, mbEscapeQuotes(artist), mbEscapeQuotes(track))
	case domain.ResultKindArtist:
		return artist
	default:
		return artist + " " + track
	}
}

func (a *MusicBrainzAdapter) searchKind(ctx context.Context, query string, kind domain.ResultKind) ([]domain.SearchResult, error) {
	entity := mbEntity(kind)
	u := fmt.Sprintf("https://musicbrainz.org/ws/2/%s/?query=%s&fmt=json&limit=15",
		entity, url.QueryEscape(query))
	if kind == domain.ResultKindTrack {
		u += "&inc=isrcs"
	}

	if err := a.limiter.wait(ctx); err != nil {
		return nil, err
	}
	status, rawBody, err := getBytes(ctx, a.client, u,
		withHeader("User-Agent", a.userAgent),
		withHeader("Accept", "application/json"))
	if err != nil {
		return nil, fmt.Errorf("musicbrainz status %d: %w", status, err)
	}

	var results []domain.SearchResult
	switch kind {
	case domain.ResultKindTrack:
		var body mbRecordingResponse
		if err := json.Unmarshal(rawBody, &body); err != nil {
			return nil, err
		}
		for _, rec := range body.Recordings {
			results = append(results, mapMBRecording(rec))
		}
	case domain.ResultKindArtist:
		var body mbArtistResponse
		if err := json.Unmarshal(rawBody, &body); err != nil {
			return nil, err
		}
		for _, art := range body.Artists {
			results = append(results, mapMBArtist(art))
		}
	case domain.ResultKindAlbum:
		var body mbReleaseGroupResponse
		if err := json.Unmarshal(rawBody, &body); err != nil {
			return nil, err
		}
		for _, rg := range body.ReleaseGroups {
			results = append(results, mapMBReleaseGroup(rg))
		}
	}
	return results, nil
}

func mbEntity(kind domain.ResultKind) string {
	switch kind {
	case domain.ResultKindTrack:
		return "recording"
	case domain.ResultKindAlbum:
		return "release-group"
	case domain.ResultKindArtist:
		return "artist"
	default:
		return "recording"
	}
}

func mapMBRecording(rec mbRecording) domain.SearchResult {
	extras := make(map[string]any)

	var subtitle string
	if len(rec.ArtistCredit) > 0 {
		subtitle = rec.ArtistCredit[0].Name
	}

	if feats := domain.FeaturedArtistsToExtras(extractMBFeatured(rec.ArtistCredit)); feats != nil {
		extras["featured_artists"] = feats
	}

	r := domain.NewProviderResult(domain.ResultKindTrack, rec.Title, subtitle, "",
		domain.SourceRef{Provider: domain.ProviderMusicBrainz, ExternalID: rec.ID, URL: "https://musicbrainz.org/recording/" + rec.ID},
		extras)
	r.MBID = rec.ID
	if len(rec.ISRCs) > 0 {
		r.ISRC = rec.ISRCs[0]
	}
	if rec.LengthMs > 0 {
		r.Duration = rec.LengthMs / 1000
		extras["duration"] = r.Duration
	}
	return r
}

func mapMBArtist(art mbArtistItem) domain.SearchResult {
	extras := make(map[string]any)
	if art.Disambiguation != "" {
		extras["disambiguation"] = art.Disambiguation
	}
	if art.Type != "" {
		extras["artist_type"] = art.Type
	}
	if art.Area != nil && art.Area.Name != "" {
		extras["area"] = art.Area.Name
	}
	if len(art.Tags) > 0 {
		names := make([]string, 0, len(art.Tags))
		for _, tag := range art.Tags {
			if tag.Name != "" {
				names = append(names, tag.Name)
			}
		}
		if len(names) > 0 {
			extras["mb_tags"] = strings.Join(names, ", ")
		}
	}

	r := domain.NewProviderResult(domain.ResultKindArtist, art.Name, "", "",
		domain.SourceRef{Provider: domain.ProviderMusicBrainz, ExternalID: art.ID, URL: "https://musicbrainz.org/artist/" + art.ID},
		extras)
	r.MBID = art.ID
	return r
}

func mapMBReleaseGroup(rg mbReleaseGroup) domain.SearchResult {
	var subtitle string
	if len(rg.ArtistCredit) > 0 {
		subtitle = rg.ArtistCredit[0].Name
	}

	var extras map[string]any
	if len(rg.SecondaryTypes) > 0 {
		extras = map[string]any{}
		extras["secondary_types"] = strings.ToLower(strings.Join(rg.SecondaryTypes, ", "))
	}

	r := domain.NewProviderResult(domain.ResultKindAlbum, rg.Title, subtitle, "",
		domain.SourceRef{Provider: domain.ProviderMusicBrainz, ExternalID: rg.ID, URL: "https://musicbrainz.org/release-group/" + rg.ID},
		extras)
	r.MBID = rg.ID
	r.RecordType = domain.RecordType(strings.ToLower(strings.TrimSpace(rg.PrimaryType)))
	r.ReleaseDate = rg.FirstReleaseDate
	return r
}

type mbRecordingResponse struct {
	Recordings []mbRecording `json:"recordings"`
}

type mbRecording struct {
	ID           string        `json:"id"`
	Title        string        `json:"title"`
	LengthMs     int           `json:"length"`
	ISRCs        []string      `json:"isrcs"`
	ArtistCredit []mbArtistRef `json:"artist-credit"`
}

type mbArtistRef struct {
	Name       string        `json:"name"`
	JoinPhrase string        `json:"joinphrase"`
	Artist     *mbArtistLink `json:"artist,omitempty"`
}

type mbArtistLink struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type mbArtistResponse struct {
	Artists []mbArtistItem `json:"artists"`
}

type mbArtistItem struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	Disambiguation string     `json:"disambiguation"`
	Type           string     `json:"type"`
	Area           *mbArea    `json:"area"`
	Tags           []mbTag    `json:"tags"`
	LifeSpan       mbLifeSpan `json:"life-span"`
}

type mbArea struct {
	Name string `json:"name"`
}

type mbTag struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type mbLifeSpan struct {
	Begin string `json:"begin"`
}

type mbReleaseGroupResponse struct {
	ReleaseGroups     []mbReleaseGroup `json:"release-groups"`
	ReleaseGroupCount int              `json:"release-group-count"`
}

type mbReleaseGroup struct {
	ID               string        `json:"id"`
	Title            string        `json:"title"`
	PrimaryType      string        `json:"primary-type"`
	SecondaryTypes   []string      `json:"secondary-types"`
	FirstReleaseDate string        `json:"first-release-date"`
	ArtistCredit     []mbArtistRef `json:"artist-credit"`
}

func (a *MusicBrainzAdapter) getJSON(ctx context.Context, u string, out any) error {
	if err := a.limiter.wait(ctx); err != nil {
		return err
	}
	return getJSON(ctx, a.client, u, out,
		withHeader("User-Agent", a.userAgent),
		withHeader("Accept", "application/json"))
}

func (a *MusicBrainzAdapter) fetchRecordingMatches(ctx context.Context, query string) ([]mbRecording, error) {
	u := fmt.Sprintf("https://musicbrainz.org/ws/2/recording/?query=%s&fmt=json&limit=10",
		url.QueryEscape(query))
	var body mbRecordingResponse
	if err := a.getJSON(ctx, u, &body); err != nil {
		return nil, err
	}
	return body.Recordings, nil
}
