package coverartarchive

import (
	"altune/go-api/internal/search/internal/adapters/providers/providerrequests"
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/internal/domain"
	"context"
	"fmt"
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

	req, err := providerrequests.NewHeadRequest(ctx, u, providerrequests.WithHeader("Accept", "image/*"))
	if err != nil {
		return providerrequests.ArtworkFailure(domain.ProviderKeyCoverArtArchive, err)
	}

	resp, err := providerrequests.Send(r.client, req)
	if err != nil {
		return providerrequests.ArtworkFailure(domain.ProviderKeyCoverArtArchive, err)
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

	return providerrequests.ArtworkFailure(domain.ProviderKeyCoverArtArchive, providerrequests.StatusError{Status: resp.StatusCode})
}

func (*CoverArtArchiveResolver) ArtworkSource() domain.ProviderKey {
	return domain.ProviderKeyCoverArtArchive
}

type CoverArtArchiveIdentityResolver struct {
	*CoverArtArchiveResolver
}

func NewCoverArtArchiveIdentityResolver(r *CoverArtArchiveResolver) CoverArtArchiveIdentityResolver {
	return CoverArtArchiveIdentityResolver{CoverArtArchiveResolver: r}
}

func (r CoverArtArchiveIdentityResolver) ResolveByIdentity(ctx context.Context, kind domain.ResultKind, id ports.ArtworkIdentity) (string, error) {
	return r.Resolve(ctx, kind, "", "", id.MBID)
}

var _ ports.IdentityArtworkLookup = CoverArtArchiveIdentityResolver{}
