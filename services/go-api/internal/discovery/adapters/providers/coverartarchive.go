package providers

import (
	"context"
	"fmt"
	"log/slog"
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

	// front-1200 is the high-fidelity hero size (CAA also serves 250/500). This
	// resolver feeds the detail-open hero — the same path iTunes serves at 1500px
	// — so 1200 is the right tier; the archive.org CDN serves it on the same
	// redirect with no extra call. (Live-probed 2026-06-23, see the audit.)
	u := fmt.Sprintf("https://coverartarchive.org/release-group/%s/front-1200", mbid)

	req, err := http.NewRequestWithContext(ctx, "HEAD", u, nil)
	if err != nil {
		return "", nil
	}
	req.Header.Set("Accept", "image/*")

	resp, err := r.client.Do(req)
	if err != nil {
		slog.DebugContext(ctx, "coverartarchive.request_failed", "mbid", mbid, "error", err)
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

func (*CoverArtArchiveResolver) ArtworkSource() string { return "coverartarchive" }
