package genius

import (
	"altune/go-api/internal/discovery/adapters/providers/providerhttp"
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

var _ ports.GeniusSongCatalog = (*SongCatalog)(nil)

const songsPerPage = 50

type SongCatalog struct {
	client      *http.Client
	accessToken string
}

func NewSongCatalog(client *http.Client, accessToken string) *SongCatalog {
	return &SongCatalog{client: client, accessToken: accessToken}
}

type geniusArtist struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	ImageURL    string `json:"image_url"`
	HeaderImage string `json:"header_image_url"`
}

type geniusSong struct {
	ID              int          `json:"id"`
	Title           string       `json:"title"`
	URL             string       `json:"url"`
	ReleaseDate     *string      `json:"release_date"`
	SongArtImageURL string       `json:"song_art_image_url"`
	HeaderImageURL  string       `json:"header_image_url"`
	PrimaryArtist   geniusArtist `json:"primary_artist"`
}

func (c *SongCatalog) FindArtistID(ctx context.Context, artistName string, knownTitles []string) (int, bool, error) {
	queries := []string{artistName}
	for i, title := range knownTitles {
		if i >= maxHintSearches {
			break
		}
		queries = append(queries, artistName+" "+title)
	}
	want := textnorm.NormalizeForIdentity(artistName)
	for _, q := range queries {
		hits, err := c.searchHits(ctx, q)
		if err != nil {
			return 0, false, err
		}
		for _, hit := range hits {
			if artist := hit.PrimaryArtist; artist.ID != 0 && textnorm.NormalizeForIdentity(artist.Name) == want {
				return artist.ID, true, nil
			}
		}
	}
	return 0, false, nil
}

func (c *SongCatalog) searchHits(ctx context.Context, query string) ([]geniusSong, error) {
	var body struct {
		Response struct {
			Hits []struct {
				Result geniusSong `json:"result"`
			} `json:"hits"`
		} `json:"response"`
	}
	if err := c.get(ctx, "/search?q="+url.QueryEscape(query), &body); err != nil {
		return nil, err
	}
	songs := make([]geniusSong, len(body.Response.Hits))
	for i, hit := range body.Response.Hits {
		songs[i] = hit.Result
	}
	return songs, nil
}

func (c *SongCatalog) ArtistSongs(ctx context.Context, artistID, maxPages int) ([]domain.UnreleasedSong, error) {
	var songs []domain.UnreleasedSong
	for page := 1; page > 0 && page <= maxPages; {
		pageSongs, next, err := c.songsPage(ctx, artistID, page)
		if err != nil {
			return nil, err
		}
		for _, s := range pageSongs {
			if s.PrimaryArtist.ID == artistID {
				songs = append(songs, toUnreleasedSong(s))
			}
		}
		page = next
	}
	return songs, nil
}

func (c *SongCatalog) songsPage(ctx context.Context, artistID, page int) (songs []geniusSong, nextPage int, err error) {
	var body struct {
		Response struct {
			Songs    []geniusSong `json:"songs"`
			NextPage *int         `json:"next_page"`
		} `json:"response"`
	}
	path := fmt.Sprintf("/artists/%d/songs?sort=popularity&per_page=%d&page=%d", artistID, songsPerPage, page)
	if err := c.get(ctx, path, &body); err != nil {
		return nil, 0, err
	}
	if body.Response.NextPage != nil {
		nextPage = *body.Response.NextPage
	}
	return body.Response.Songs, nextPage, nil
}

func (c *SongCatalog) HasMedia(ctx context.Context, songID int) (bool, error) {
	var body struct {
		Response struct {
			Song struct {
				Media []struct {
					Provider string `json:"provider"`
				} `json:"media"`
			} `json:"song"`
		} `json:"response"`
	}
	if err := c.get(ctx, fmt.Sprintf("/songs/%d", songID), &body); err != nil {
		return false, err
	}
	for _, m := range body.Response.Song.Media {
		if m.Provider == "youtube" || m.Provider == "soundcloud" {
			return true, nil
		}
	}
	return false, nil
}

func (c *SongCatalog) get(ctx context.Context, path string, dst any) error {
	return providerhttp.GetJSON(ctx, c.client, "https://api.genius.com"+path, dst,
		providerhttp.WithHeader("Authorization", "Bearer "+c.accessToken))
}

func toUnreleasedSong(s geniusSong) domain.UnreleasedSong {
	song := domain.UnreleasedSong{
		GeniusID:  s.ID,
		Title:     s.Title,
		GeniusURL: s.URL,
		CoverURL:  songSpecificCover(s),
	}
	if s.ReleaseDate != nil {
		song.ReleaseDate = *s.ReleaseDate
	}
	return song
}

func songSpecificCover(s geniusSong) string {
	for _, img := range []string{s.SongArtImageURL, s.HeaderImageURL} {
		if img != "" && !isPlaceholderImage(img) && img != s.PrimaryArtist.ImageURL && img != s.PrimaryArtist.HeaderImage {
			return img
		}
	}
	return ""
}

func isPlaceholderImage(img string) bool {
	return strings.Contains(img, "default") || strings.Contains(img, "no_image")
}
