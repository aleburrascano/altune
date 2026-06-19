package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

type MusicBrainzAdapter struct {
	client    *http.Client
	userAgent string
	mu        sync.Mutex
	lastReq   time.Time
}

func NewMusicBrainzAdapter(client *http.Client, userAgent string) *MusicBrainzAdapter {
	return &MusicBrainzAdapter{client: client, userAgent: userAgent}
}

// rateLimit enforces MB's 1 req/sec policy. Call before every HTTP request.
func (a *MusicBrainzAdapter) rateLimit() {
	a.mu.Lock()
	defer a.mu.Unlock()
	since := time.Since(a.lastReq)
	if since < time.Second {
		time.Sleep(time.Second - since)
	}
	a.lastReq = time.Now()
}

func (a *MusicBrainzAdapter) Name() domain.ProviderName { return domain.ProviderMusicBrainz }

func (a *MusicBrainzAdapter) SupportedKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{
		domain.ResultKindTrack:  true,
		domain.ResultKindAlbum:  true,
		domain.ResultKindArtist: true,
	}
}

func (a *MusicBrainzAdapter) SearchTimeout() time.Duration {
	return 4 * time.Second
}

func (a *MusicBrainzAdapter) Search(ctx context.Context, query string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	var results []domain.SearchResult

	searchOrder := []domain.ResultKind{
		domain.ResultKindArtist,
		domain.ResultKindTrack,
		domain.ResultKindAlbum,
	}

	for _, kind := range searchOrder {
		if !kinds[kind] || !a.SupportedKinds()[kind] {
			continue
		}

		items, err := a.searchKind(ctx, query, kind)
		if err != nil {
			slog.WarnContext(ctx, "musicbrainz.search_kind_failed",
				"kind", kind.String(), "query", query, "error", err)
			continue
		}
		results = append(results, items...)
	}

	return results, nil
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
		return fmt.Sprintf(`artist:"%s" AND recording:"%s"`, artist, track)
	case domain.ResultKindAlbum:
		return fmt.Sprintf(`artist:"%s" AND release:"%s"`, artist, track)
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

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", a.userAgent)
	req.Header.Set("Accept", "application/json")
	a.rateLimit()

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("musicbrainz returned %d", resp.StatusCode)
	}

	var results []domain.SearchResult
	switch kind {
	case domain.ResultKindTrack:
		var body mbRecordingResponse
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			return nil, err
		}
		for _, rec := range body.Recordings {
			results = append(results, mapMBRecording(rec))
		}
	case domain.ResultKindArtist:
		var body mbArtistResponse
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			return nil, err
		}
		for _, art := range body.Artists {
			results = append(results, mapMBArtist(art))
		}
	case domain.ResultKindAlbum:
		var body mbReleaseGroupResponse
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
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
	extras["mbid"] = rec.ID
	if len(rec.ISRCs) > 0 {
		extras["isrc"] = rec.ISRCs[0]
	}

	var subtitle string
	if len(rec.ArtistCredit) > 0 {
		subtitle = rec.ArtistCredit[0].Name
	}

	return domain.SearchResult{
		Kind:       domain.ResultKindTrack,
		Title:      rec.Title,
		Subtitle:   subtitle,
		Confidence: domain.ConfidenceLow,
		Sources: []domain.SourceRef{{
			Provider:   domain.ProviderMusicBrainz,
			ExternalID: rec.ID,
			URL:        "https://musicbrainz.org/recording/" + rec.ID,
		}},
		Extras: extras,
	}
}

func mapMBArtist(art mbArtistItem) domain.SearchResult {
	extras := make(map[string]any)
	extras["mbid"] = art.ID
	if art.Disambiguation != "" {
		extras["disambiguation"] = art.Disambiguation
	}

	return domain.SearchResult{
		Kind:       domain.ResultKindArtist,
		Title:      art.Name,
		Confidence: domain.ConfidenceLow,
		Sources: []domain.SourceRef{{
			Provider:   domain.ProviderMusicBrainz,
			ExternalID: art.ID,
			URL:        "https://musicbrainz.org/artist/" + art.ID,
		}},
		Extras: extras,
	}
}

func mapMBReleaseGroup(rg mbReleaseGroup) domain.SearchResult {
	extras := make(map[string]any)
	extras["mbid"] = rg.ID

	var subtitle string
	if len(rg.ArtistCredit) > 0 {
		subtitle = rg.ArtistCredit[0].Name
	}

	return domain.SearchResult{
		Kind:       domain.ResultKindAlbum,
		Title:      rg.Title,
		Subtitle:   subtitle,
		Confidence: domain.ConfidenceLow,
		Sources: []domain.SourceRef{{
			Provider:   domain.ProviderMusicBrainz,
			ExternalID: rg.ID,
			URL:        "https://musicbrainz.org/release-group/" + rg.ID,
		}},
		Extras: extras,
	}
}

type mbRecordingResponse struct {
	Recordings []mbRecording `json:"recordings"`
}

type mbRecording struct {
	ID           string          `json:"id"`
	Title        string          `json:"title"`
	ISRCs        []string        `json:"isrcs"`
	ArtistCredit []mbArtistRef   `json:"artist-credit"`
}

type mbArtistRef struct {
	Name string `json:"name"`
}

type mbArtistResponse struct {
	Artists []mbArtistItem `json:"artists"`
}

type mbArtistItem struct {
	ID             string     `json:"id"`
	Name           string     `json:"name"`
	Disambiguation string     `json:"disambiguation"`
	LifeSpan       mbLifeSpan `json:"life-span"`
}

type mbLifeSpan struct {
	Begin string `json:"begin"`
}

type mbReleaseGroupResponse struct {
	ReleaseGroups []mbReleaseGroup `json:"release-groups"`
}

type mbReleaseGroup struct {
	ID              string        `json:"id"`
	Title           string        `json:"title"`
	PrimaryType     string        `json:"primary-type"`
	FirstReleaseDate string       `json:"first-release-date"`
	ArtistCredit    []mbArtistRef `json:"artist-credit"`
}

// ValidateArtistAlbums cross-references albums against MusicBrainz.
// Searches MB for the artist by name, picks the best match, queries
// their release-groups, and splits input albums into confirmed
// (title matches an MB release) vs unconfirmed.
func (a *MusicBrainzAdapter) ValidateArtistAlbums(
	ctx context.Context,
	artistName string,
	albums []domain.SearchResult,
) (*ports.AlbumValidationResult, error) {
	mbid, err := a.resolveArtistMBID(ctx, artistName)
	if err != nil {
		slog.WarnContext(ctx, "mb.resolve_mbid_failed", "artist", artistName, "error", err)
		return &ports.AlbumValidationResult{
			Confirmed:   albums,
			Unconfirmed: nil,
		}, nil
	}
	if mbid == "" {
		slog.InfoContext(ctx, "mb.no_mbid_found", "artist", artistName)
		return &ports.AlbumValidationResult{
			Confirmed:   albums,
			Unconfirmed: nil,
		}, nil
	}
	slog.InfoContext(ctx, "mb.artist_resolved", "artist", artistName, "mbid", mbid)

	releases, err := a.fetchReleaseGroups(ctx, mbid)
	if err != nil {
		slog.WarnContext(ctx, "mb.release_groups_failed", "mbid", mbid, "error", err)
		return &ports.AlbumValidationResult{
			Confirmed:   albums,
			Unconfirmed: nil,
			ArtistMBID:  mbid,
		}, nil
	}

	mbTitles := make(map[string]bool, len(releases))
	for _, rg := range releases {
		mbTitles[normForMatch(rg.Title)] = true
	}

	var confirmed, unconfirmed []domain.SearchResult
	for _, album := range albums {
		if mbTitles[normForMatch(album.Title)] {
			confirmed = append(confirmed, album)
		} else {
			unconfirmed = append(unconfirmed, album)
		}
	}

	slog.InfoContext(ctx, "mb.album_validation",
		"artist", artistName, "mbid", mbid,
		"mb_releases", len(releases),
		"confirmed", len(confirmed),
		"unconfirmed", len(unconfirmed),
	)

	return &ports.AlbumValidationResult{
		Confirmed:   confirmed,
		Unconfirmed: unconfirmed,
		ArtistMBID:  mbid,
	}, nil
}

func (a *MusicBrainzAdapter) ResolveArtistIdentity(ctx context.Context, name string) (*ports.ArtistIdentity, error) {
	artists, err := a.fetchArtistMatches(ctx, name)
	if err != nil {
		return nil, err
	}
	nameNorm := normForMatch(name)

	var first *mbArtistItem
	candidateCount := 0
	for i := range artists {
		if normForMatch(artists[i].Name) == nameNorm {
			candidateCount++
			if first == nil {
				first = &artists[i]
			}
		}
	}
	if first == nil {
		return nil, nil
	}
	if candidateCount > 1 {
		slog.InfoContext(ctx, "mb.multiple_name_matches",
			"name", name, "candidates", candidateCount,
			"picked_mbid", first.ID,
			"picked_disambiguation", first.Disambiguation,
		)
	}
	birthYear := parseBirthYear(first.LifeSpan.Begin)
	return &ports.ArtistIdentity{
		MBID:           first.ID,
		Disambiguation: first.Disambiguation,
		BirthYear:      birthYear,
	}, nil
}

func parseBirthYear(begin string) int {
	if len(begin) < 4 {
		return 0
	}
	year := 0
	for _, c := range begin[:4] {
		if c < '0' || c > '9' {
			return 0
		}
		year = year*10 + int(c-'0')
	}
	return year
}

func (a *MusicBrainzAdapter) resolveArtistMBID(ctx context.Context, name string) (string, error) {
	id, err := a.ResolveArtistIdentity(ctx, name)
	if err != nil {
		return "", err
	}
	if id == nil {
		return "", nil
	}
	return id.MBID, nil
}

func (a *MusicBrainzAdapter) fetchArtistMatches(ctx context.Context, name string) ([]mbArtistItem, error) {
	u := fmt.Sprintf("https://musicbrainz.org/ws/2/artist/?query=%s&fmt=json&limit=5",
		url.QueryEscape(name))

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", a.userAgent)
	req.Header.Set("Accept", "application/json")
	a.rateLimit()

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("musicbrainz artist search returned %d", resp.StatusCode)
	}

	var body mbArtistResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	return body.Artists, nil
}

func (a *MusicBrainzAdapter) fetchReleaseGroups(ctx context.Context, mbid string) ([]mbReleaseGroup, error) {
	u := fmt.Sprintf(
		"https://musicbrainz.org/ws/2/release-group?artist=%s&type=album%%7Cep%%7Csingle&fmt=json&limit=100",
		url.QueryEscape(mbid))

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", a.userAgent)
	req.Header.Set("Accept", "application/json")
	a.rateLimit()

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("musicbrainz release-groups returned %d", resp.StatusCode)
	}

	var body mbReleaseGroupResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	return body.ReleaseGroups, nil
}

func normForMatch(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
