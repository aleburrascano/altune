package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"
)

type DiscogsAdapter struct {
	client    *http.Client
	token     string
	userAgent string
	mu        sync.Mutex
	lastReq   time.Time
}

func NewDiscogsAdapter(client *http.Client, token, userAgent string) *DiscogsAdapter {
	return &DiscogsAdapter{client: client, token: token, userAgent: userAgent}
}

func (a *DiscogsAdapter) rateLimit() {
	a.mu.Lock()
	since := time.Since(a.lastReq)
	a.lastReq = time.Now()
	a.mu.Unlock()
	if since < time.Second {
		time.Sleep(time.Second - since)
	}
}

func (a *DiscogsAdapter) Resolve(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid string) (string, error) {
	if kind != domain.ResultKindArtist {
		return "", nil
	}

	artists, err := a.searchArtists(ctx, title)
	if err != nil || len(artists) == 0 {
		return "", nil
	}

	best := artists[0]
	if len(artists) > 1 {
		slog.DebugContext(ctx, "discogs.multiple_artists",
			"name", title, "count", len(artists))
	}

	detail, err := a.fetchArtistDetail(ctx, best.ID)
	if err != nil || len(detail.Images) == 0 {
		return "", nil
	}

	for _, img := range detail.Images {
		if img.Type == "primary" && img.URI != "" {
			return img.URI, nil
		}
	}
	if detail.Images[0].URI != "" {
		return detail.Images[0].URI, nil
	}
	return "", nil
}

// ResolveByIdentity fetches the primary image of the exact bridged Discogs
// artist — identity-correct, no name search. This is the path that gets the
// right face for same-name artists ("Che (38)" vs the seven other Ches): the
// merge's MB→Discogs bridge already proved which Discogs entry this entity is.
func (a *DiscogsAdapter) ResolveByIdentity(ctx context.Context, kind domain.ResultKind, id ports.ArtworkIdentity) (string, error) {
	if kind != domain.ResultKindArtist {
		return "", nil
	}
	discogsID, err := strconv.Atoi(id.ExternalIDs["discogs"])
	if err != nil || discogsID == 0 {
		return "", nil
	}
	detail, err := a.fetchArtistDetail(ctx, discogsID)
	if err != nil || len(detail.Images) == 0 {
		return "", nil
	}
	for _, img := range detail.Images {
		if img.Type == "primary" && img.URI != "" {
			return img.URI, nil
		}
	}
	if detail.Images[0].URI != "" {
		return detail.Images[0].URI, nil
	}
	return "", nil
}

func (a *DiscogsAdapter) ResolveDiscogsArtist(ctx context.Context, name string, albumTitles []string) (*ports.DiscogsArtistInfo, error) {
	artists, err := a.searchArtists(ctx, name)
	if err != nil {
		return nil, err
	}
	if len(artists) == 0 {
		return nil, nil
	}
	if len(artists) == 1 {
		genre := strings.Join(artists[0].Genre, ", ")
		country := artists[0].Country
		detail, err := a.fetchArtistDetail(ctx, artists[0].ID)
		if err != nil {
			return &ports.DiscogsArtistInfo{ID: artists[0].ID, Name: artists[0].Title, Genre: genre, Country: country}, nil
		}
		return &ports.DiscogsArtistInfo{ID: detail.ID, Name: detail.Name, Genre: genre, Country: country}, nil
	}

	albumSet := make(map[string]bool, len(albumTitles))
	for _, t := range albumTitles {
		albumSet[textnorm.NormalizeForMatch(t)] = true
	}

	var bestArtist discogsSearchResult
	bestOverlap := -1
	for _, artist := range artists {
		releases, err := a.fetchArtistReleases(ctx, artist.ID, 50)
		if err != nil {
			continue
		}
		overlap := 0
		for _, rel := range releases {
			if albumSet[textnorm.NormalizeForMatch(rel.Title)] {
				overlap++
			}
		}
		if overlap > bestOverlap {
			bestOverlap = overlap
			bestArtist = artist
		}
	}

	if bestOverlap < 0 {
		bestArtist = artists[0]
	}

	slog.InfoContext(ctx, "discogs.artist_resolved",
		"name", name, "discogs_id", bestArtist.ID,
		"overlap", bestOverlap, "candidates", len(artists))

	genre := strings.Join(bestArtist.Genre, ", ")
	country := bestArtist.Country

	detail, err := a.fetchArtistDetail(ctx, bestArtist.ID)
	if err != nil {
		return &ports.DiscogsArtistInfo{ID: bestArtist.ID, Name: bestArtist.Title, Genre: genre, Country: country, Overlap: bestOverlap}, nil
	}
	return &ports.DiscogsArtistInfo{ID: detail.ID, Name: detail.Name, Genre: genre, Country: country, Overlap: bestOverlap}, nil
}

func (a *DiscogsAdapter) FetchArtistReleases(ctx context.Context, discogsID int) ([]ports.DiscogsRelease, error) {
	releases, err := a.fetchArtistReleases(ctx, discogsID, 100)
	if err != nil {
		return nil, err
	}
	out := make([]ports.DiscogsRelease, len(releases))
	for i, r := range releases {
		out[i] = ports.DiscogsRelease{Title: r.Title, Year: r.Year, Type: r.Type}
	}
	return out, nil
}

type discogsSearchResponse struct {
	Results []discogsSearchResult `json:"results"`
}

type discogsSearchResult struct {
	ID      int      `json:"id"`
	Title   string   `json:"title"`
	Type    string   `json:"type"`
	Genre   []string `json:"genre"`
	Country string   `json:"country"`
}

type discogsImage struct {
	Type string `json:"type"`
	URI  string `json:"uri"`
}

type discogsArtistDetail struct {
	ID      int            `json:"id"`
	Name    string         `json:"name"`
	Profile string         `json:"profile"`
	Images  []discogsImage `json:"images"`
}

type discogsRelease struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
	Year  int    `json:"year"`
	Type  string `json:"type"`
}

type discogsReleasesResponse struct {
	Releases []discogsRelease `json:"releases"`
}

func (a *DiscogsAdapter) searchArtists(ctx context.Context, name string) ([]discogsSearchResult, error) {
	u := fmt.Sprintf("https://api.discogs.com/database/search?type=artist&q=%s&per_page=5",
		url.QueryEscape(name))

	body, err := a.doGet(ctx, u)
	if err != nil {
		return nil, err
	}

	var resp discogsSearchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return resp.Results, nil
}

func (a *DiscogsAdapter) fetchArtistDetail(ctx context.Context, artistID int) (*discogsArtistDetail, error) {
	u := fmt.Sprintf("https://api.discogs.com/artists/%d", artistID)

	body, err := a.doGet(ctx, u)
	if err != nil {
		return nil, err
	}

	var detail discogsArtistDetail
	if err := json.Unmarshal(body, &detail); err != nil {
		return nil, err
	}
	return &detail, nil
}

func (a *DiscogsAdapter) fetchArtistReleases(ctx context.Context, artistID, perPage int) ([]discogsRelease, error) {
	u := fmt.Sprintf("https://api.discogs.com/artists/%d/releases?sort=year&sort_order=desc&per_page=%d",
		artistID, perPage)

	body, err := a.doGet(ctx, u)
	if err != nil {
		return nil, err
	}

	var resp discogsReleasesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return resp.Releases, nil
}

func (a *DiscogsAdapter) doGet(ctx context.Context, rawURL string) ([]byte, error) {
	a.rateLimit()
	status, body, err := getBytes(ctx, a.client, rawURL,
		withHeader("Authorization", "Discogs token="+a.token),
		withHeader("User-Agent", a.userAgent))
	if status == 429 {
		slog.WarnContext(ctx, "discogs.rate_limited", "url", rawURL)
		return nil, fmt.Errorf("discogs rate limited")
	}
	if err != nil {
		return nil, err
	}
	return body, nil
}
