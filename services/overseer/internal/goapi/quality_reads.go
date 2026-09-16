package goapi

import (
	"context"
	"time"
)

// adminDiscographyQualityPath is go-api's operator discography structural-quality
// endpoint, mounted under the operator-guarded "/admin" group
// (internal/admin/handler/admin_handler.go). The pinned seam is
// GET /admin/quality/discography?by=artist&window_days=30; go-api defaults the
// grouping to "artist" and the window to 30 days when those params are absent,
// exactly matching the pin — so this reader calls the bare path through the
// guarded get primitive (which joins path segments structurally and cannot carry
// a query string) and receives the identical pinned response.
const adminDiscographyQualityPath = "/admin/quality/discography"

// DiscographyQuality mirrors go-api's discography structural-quality response
// from GET /admin/quality/discography: the cross-provider disagreement verdict
// computed at the artist-content merge, grouped by artist over a bounded window.
// The verdict is computed in go-api; this is a pure read of the served result —
// the Overseer never re-fetches providers or re-runs the merge. Unknown fields a
// newer go-api adds are ignored, so the mirror tolerates version skew.
type DiscographyQuality struct {
	WindowDays int               `json:"window_days"`
	GroupBy    string            `json:"group_by"`
	Cases      []DiscographyCase `json:"cases"`
}

// DiscographyCase is one artist's structural-quality case. Artist, ArtistRef and
// the provider names are watched-app data — stored raw here and HTML-escaped only
// at render time, never trusted as markup. SingleProvider is the
// contamination-suspect count (releases exactly one provider supplied); it is a
// hint for the owner to judge, never an assertion that the discography is wrong.
type DiscographyCase struct {
	Artist         string         `json:"artist"`
	ArtistRef      string         `json:"artist_ref"`
	Releases       int            `json:"releases"`
	SingleProvider int            `json:"single_provider"`
	ProviderCounts map[string]int `json:"provider_counts"`
	LastSeen       time.Time      `json:"last_seen"`
}

// AdminDiscographyQuality fetches GET /admin/quality/discography, go-api's
// operator discography structural-quality verdict, decoded into
// DiscographyQuality. It reuses the read primitive, so the operator bearer, the
// host pin, the bounded body and the timeout all apply, and it is a pure read:
// nothing here writes, commands or re-runs any go-api pipeline. With no by=
// param go-api defaults the grouping to "artist" — the default worst-first view.
func (c *Client) AdminDiscographyQuality(ctx context.Context) (DiscographyQuality, error) {
	var out DiscographyQuality
	if err := c.get(ctx, adminDiscographyQualityPath, &out); err != nil {
		return DiscographyQuality{}, err
	}
	return out, nil
}
