package goapi

import (
	"context"
	"time"
)

// observeDiscographyQualityPath is go-api's operator discography structural-quality
// endpoint, mounted under the observe-guarded "/observe" group
// (internal/app/observe_wiring.go). The pinned seam is
// GET /observe/quality/discography?by=artist&window_days=30; go-api defaults the
// grouping to "artist" and the window to 30 days when those params are absent,
// exactly matching the pin — so this reader calls the bare path through the
// guarded get primitive (which joins path segments structurally and cannot carry
// a query string) and receives the identical pinned response.
const observeDiscographyQualityPath = "/observe/quality/discography"

// DiscographyQuality mirrors go-api's discography structural-quality response
// from GET /observe/quality/discography: the cross-provider disagreement verdict
// computed at the artist-content merge, grouped by artist over a bounded window.
// The verdict is computed in go-api; this is a pure read of the served result —
// the Overseer never re-fetches providers or re-runs the merge. Unknown fields a
// newer go-api adds are ignored, so the mirror tolerates version skew.
type DiscographyQuality struct {
	WindowDays int               `json:"window_days"`
	GroupBy    string            `json:"group_by"`
	Cases      []DiscographyCase `json:"cases"`
	// SuspectRate is go-api's windowed headline in [0,1]: the share of real
	// discography opens whose top release-suspect fired. LastSampleAt is the most
	// recent real open's time, so the panel can show the headline's freshness. Both
	// are computed over server-emitted opens only in go-api; the Overseer renders the
	// served number, it never recomputes it. Absent on an older go-api and decoding
	// to their zero values (version-skew tolerant).
	SuspectRate  float64   `json:"suspect_rate"`
	LastSampleAt time.Time `json:"last_sample_at"`
}

// DiscographyCase is one artist's structural-quality case. Artist, ArtistRef and
// the provider names are watched-app data — stored raw here and HTML-escaped only
// at render time, never trusted as markup. SingleProvider counts releases exactly
// one provider supplied; SingleProviderNoID counts how many of those also lack a
// shared id — the real contamination suspects the worst-first order ranks on,
// since a lone-provider release still carrying a strong/verified id is not a
// suspect. Both are hints for the owner to judge, never an assertion that the
// discography is wrong. SingleProviderNoID is absent on an older go-api and decodes
// to 0 (version-skew tolerant).
type DiscographyCase struct {
	Artist             string         `json:"artist"`
	ArtistRef          string         `json:"artist_ref"`
	Releases           int            `json:"releases"`
	SingleProvider     int            `json:"single_provider"`
	SingleProviderNoID int            `json:"single_provider_no_id"`
	ProviderCounts     map[string]int `json:"provider_counts"`
	LastSeen           time.Time      `json:"last_seen"`
}

// AdminDiscographyQuality fetches GET /observe/quality/discography, go-api's
// operator discography structural-quality verdict, decoded into
// DiscographyQuality. It reuses the read primitive, so the read-only bearer, the
// host pin, the bounded body and the timeout all apply, and it is a pure read:
// nothing here writes, commands or re-runs any go-api pipeline. With no by=
// param go-api defaults the grouping to "artist" — the default worst-first view.
func (c *Client) AdminDiscographyQuality(ctx context.Context) (DiscographyQuality, error) {
	var out DiscographyQuality
	if err := c.get(ctx, observeDiscographyQualityPath, &out); err != nil {
		return DiscographyQuality{}, err
	}
	return out, nil
}
