package providers

import (
	"context"
	"fmt"
	"net/http"

	"altune/go-api/internal/discovery/domain"
)

type CoverArtArchiveResolver struct {
	client *http.Client
}

func NewCoverArtArchiveResolver(client *http.Client) *CoverArtArchiveResolver {
	return &CoverArtArchiveResolver{client: client}
}

func (r *CoverArtArchiveResolver) Resolve(ctx context.Context, kind domain.ResultKind, title, subtitle string, mbid string) (string, error) {
	if mbid == "" {
		return "", nil
	}
	if kind == domain.ResultKindArtist {
		return "", nil
	}

	u := fmt.Sprintf("https://coverartarchive.org/release-group/%s/front-500", mbid)

	req, err := http.NewRequestWithContext(ctx, "HEAD", u, nil)
	if err != nil {
		return "", nil
	}
	req.Header.Set("Accept", "image/*")

	resp, err := r.client.Do(req)
	if err != nil {
		return "", nil
	}
	resp.Body.Close()

	if resp.StatusCode == 404 || resp.StatusCode == 400 {
		return "", nil
	}
	if resp.StatusCode == 307 || resp.StatusCode == 302 || resp.StatusCode == 301 {
		loc := resp.Header.Get("Location")
		if loc != "" {
			return loc, nil
		}
	}
	if resp.StatusCode == 200 {
		return u, nil
	}

	return "", nil
}
