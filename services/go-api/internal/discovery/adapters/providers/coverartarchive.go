package providers

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
)

type CoverArtArchiveResolver struct {
	client *http.Client
}

func NewCoverArtArchiveResolver(client *http.Client) *CoverArtArchiveResolver {
	return &CoverArtArchiveResolver{client: client}
}

func (r *CoverArtArchiveResolver) Resolve(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid string) (string, error) {
	if mbid == "" {
		return "", nil
	}
	if kind == domain.ResultKindArtist {
		return "", nil
	}

	u := fmt.Sprintf("https://coverartarchive.org/release-group/%s/front-1200", url.PathEscape(mbid))

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, u, http.NoBody)
	if err != nil {
		return "", nil //nolint:nilerr // intentional graceful degradation: artwork resolution is best-effort
	}
	req.Header.Set("Accept", "image/*")

	resp, err := r.client.Do(req)
	if err != nil {
		slog.DebugContext(ctx, "coverartarchive.request_failed", "mbid", mbid, "error", err)
		return "", nil
	}
	_ = resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusBadRequest {
		return "", nil
	}
	if resp.StatusCode == http.StatusTemporaryRedirect || resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusMovedPermanently {
		loc := resp.Header.Get("Location")
		if loc != "" {
			return loc, nil
		}
	}
	if resp.StatusCode == http.StatusOK {
		return u, nil
	}

	return "", nil
}

func (*CoverArtArchiveResolver) ArtworkSource() domain.ProviderKey {
	return domain.ProviderKeyCoverArtArchive
}
