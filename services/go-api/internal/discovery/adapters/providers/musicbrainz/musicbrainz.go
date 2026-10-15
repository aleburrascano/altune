package musicbrainz

import (
	"altune/go-api/internal/discovery/adapters/providers/providerhttp"
	"altune/go-api/internal/discovery/adapters/providers/providersearch"
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

const musicbrainzAPIBaseURL = "https://musicbrainz.org/ws/2"

type MusicBrainzAdapter struct {
	client    *http.Client
	userAgent string
	limiter   *providerhttp.MinIntervalLimiter

	identityMemo *mbMemo[*ports.ArtistIdentity]
	releaseMemo  *mbMemo[[]mbReleaseGroup]
	releaseSF    singleflight.Group
}

func NewMusicBrainzAdapter(client *http.Client, userAgent string) *MusicBrainzAdapter {
	return &MusicBrainzAdapter{
		client:       client,
		userAgent:    userAgent,
		limiter:      providerhttp.NewMinIntervalLimiter(time.Second),
		identityMemo: newMBMemo[*ports.ArtistIdentity](mbMemoTTL),
		releaseMemo:  newMBMemo[[]mbReleaseGroup](mbMemoTTL),
	}
}

func (a *MusicBrainzAdapter) Name() domain.ProviderName { return domain.ProviderMusicBrainz }

func (a *MusicBrainzAdapter) SupportedKinds() map[domain.ResultKind]bool {
	return providersearch.AllSearchKinds()
}

func (a *MusicBrainzAdapter) SearchTimeout() time.Duration {
	return 5 * time.Second
}

func (a *MusicBrainzAdapter) Search(ctx context.Context, query string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	return providersearch.SearchAcrossKinds(ctx, a.Name().String(), query, kinds, a.SupportedKinds(),
		func(ctx context.Context, kind domain.ResultKind) ([]domain.SearchResult, error) {
			return a.searchKind(ctx, providersearch.MbLuceneEscape(query), kind)
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

func mbStructuredQuery(artist, track string, kind domain.ResultKind) string {
	switch kind {
	case domain.ResultKindTrack:
		return "artist:" + providersearch.DslQuote(providersearch.MbLuceneEscape(artist)) + " AND recording:" + providersearch.DslQuote(providersearch.MbLuceneEscape(track))
	case domain.ResultKindAlbum:
		return "artist:" + providersearch.DslQuote(providersearch.MbLuceneEscape(artist)) + " AND release:" + providersearch.DslQuote(providersearch.MbLuceneEscape(track))
	case domain.ResultKindArtist:
		return providersearch.MbLuceneEscape(artist)
	default:
		return providersearch.MbLuceneEscape(artist) + " " + providersearch.MbLuceneEscape(track)
	}
}

func (a *MusicBrainzAdapter) searchKind(ctx context.Context, query string, kind domain.ResultKind) ([]domain.SearchResult, error) {
	entity := mbEntity(kind)
	u := fmt.Sprintf(musicbrainzAPIBaseURL+"/%s/?query=%s&fmt=json&limit=15",
		entity, url.QueryEscape(query))
	if kind == domain.ResultKindTrack {
		u += "&inc=isrcs"
	}

	if err := a.limiter.Wait(ctx); err != nil {
		return nil, err
	}
	status, rawBody, err := providerhttp.GetBytes(ctx, a.client, u,
		providerhttp.WithHeader("User-Agent", a.userAgent),
		providerhttp.WithHeader("Accept", "application/json"))
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
		extras[domain.ExtraFeaturedArtists] = feats
	}

	r := domain.NewProviderResult(domain.ResultKindTrack, recordingTitle(rec), subtitle, "",
		domain.SourceRef{Provider: domain.ProviderMusicBrainz, ExternalID: rec.ID, URL: "https://musicbrainz.org/recording/" + rec.ID},
		extras)
	r.RecordingMBID = rec.ID
	if len(rec.ISRCs) > 0 {
		r.ISRC = rec.ISRCs[0]
	}
	if rec.LengthMs > 0 {
		r.Duration = rec.LengthMs / 1000
	}
	return r
}

func recordingTitle(rec mbRecording) string {
	if note := strings.TrimSpace(rec.Disambiguation); note != "" {
		return rec.Title + " (" + note + ")"
	}
	return rec.Title
}

func mapMBArtist(art mbArtistItem) domain.SearchResult {
	extras := make(map[string]any)
	if art.Disambiguation != "" {
		extras[domain.ExtraDisambiguation] = art.Disambiguation
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
	r.ArtistMBID = art.ID
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
	r.ReleaseGroupMBID = rg.ID
	r.RecordType = domain.RecordType(strings.ToLower(strings.TrimSpace(rg.PrimaryType)))
	r.ReleaseDate = rg.FirstReleaseDate
	return r
}

type mbRecordingResponse struct {
	Recordings []mbRecording `json:"recordings"`
}

type mbRecording struct {
	ID             string        `json:"id"`
	Title          string        `json:"title"`
	Disambiguation string        `json:"disambiguation"`
	LengthMs       int           `json:"length"`
	ISRCs          []string      `json:"isrcs"`
	ArtistCredit   []mbArtistRef `json:"artist-credit"`
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
	if err := a.limiter.Wait(ctx); err != nil {
		return err
	}
	return providerhttp.GetJSON(ctx, a.client, u, out,
		providerhttp.WithHeader("User-Agent", a.userAgent),
		providerhttp.WithHeader("Accept", "application/json"))
}

func (a *MusicBrainzAdapter) fetchRecordingMatches(ctx context.Context, query string) ([]mbRecording, error) {
	u := fmt.Sprintf(musicbrainzAPIBaseURL+"/recording/?query=%s&fmt=json&limit=10",
		url.QueryEscape(providersearch.MbLuceneEscape(query)))
	var body mbRecordingResponse
	if err := a.getJSON(ctx, u, &body); err != nil {
		return nil, err
	}
	return body.Recordings, nil
}
