package providers

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

func (a *ITunesAdapter) LookupAlbum(
	ctx context.Context,
	albumTitle, artistName string,
	profile domain.ArtistIdentityProfile,
) (domain.AlbumVerdict, int64, error) {
	u := fmt.Sprintf(
		"https://itunes.apple.com/search?term=%s&entity=album&country=US&limit=5",
		url.QueryEscape(albumTitle),
	)

	req, err := http.NewRequestWithContext(ctx, "GET", u, http.NoBody)
	if err != nil {
		return domain.AlbumVerdictUnknown, 0, nil
	}
	req.Header.Set("User-Agent", itunesUserAgent)
	if err := a.limiter.wait(ctx); err != nil {
		slog.WarnContext(ctx, "itunes.lookup_album_failed", "album", albumTitle, "error", err)
		return domain.AlbumVerdictUnknown, 0, nil
	}

	resp, err := a.client.Do(req)
	if err != nil {
		slog.WarnContext(ctx, "itunes.lookup_album_failed", "album", albumTitle, "error", err)
		return domain.AlbumVerdictUnknown, 0, nil
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		return domain.AlbumVerdictUnknown, 0, nil
	}

	var body itunesResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return domain.AlbumVerdictUnknown, 0, nil
	}

	titleNorm := textnorm.NormalizeForMatch(albumTitle)
	artistNorm := textnorm.NormalizeForMatch(artistName)

	for _, item := range body.Results {
		collNorm := textnorm.NormalizeForMatch(stripITunesTypeSuffix(item.CollectionName))
		if collNorm != titleNorm {
			continue
		}

		if textnorm.NormalizeForMatch(item.ArtistName) != artistNorm {
			return domain.AlbumVerdictContamination, item.ArtistID, nil
		}

		if len(profile.GenreCluster) > 0 && item.PrimaryGenreName != "" {
			genres := strings.Split(item.PrimaryGenreName, "/")
			if !profile.HasGenreOverlap(genres) {
				return domain.AlbumVerdictContamination, item.ArtistID, nil
			}
		}

		return domain.AlbumVerdictConfirmed, item.ArtistID, nil
	}

	return domain.AlbumVerdictUnknown, 0, nil
}
