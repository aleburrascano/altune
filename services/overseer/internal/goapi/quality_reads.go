package goapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// adminDiscographyQualityPath is go-api's operator discography structural-quality
// endpoint, mounted under the operator-guarded "/admin" group
// (internal/admin/handler/admin_handler.go). The pinned seam is
// GET /admin/quality/discography?by=artist&window_days=30; go-api defaults the
// grouping to "artist" and the window to 30 days when those params are absent,
// exactly matching the pin — so this reader calls the bare path through the
// guarded get primitive (which joins path segments structurally and cannot carry
// a query string) and receives the identical pinned response. When a later slice
// needs a caller-chosen pivot, the query-capable variant is added then, not here.
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
// param go-api defaults the grouping to "artist" — this is the default worst-first
// view; the caller-chosen pivots go through AdminDiscographyQualityBy.
func (c *Client) AdminDiscographyQuality(ctx context.Context) (DiscographyQuality, error) {
	var out DiscographyQuality
	if err := c.get(ctx, adminDiscographyQualityPath, &out); err != nil {
		return DiscographyQuality{}, err
	}
	return out, nil
}

// DiscographyGroupings is the allowlist of pivot groupings the pinned seam accepts
// on by=. Constraining the pivot to this fixed set means a caller-chosen grouping
// can never smuggle an arbitrary or unbounded query into the guarded read; an
// unknown value falls back to the seam default so a hostile pivot degrades to the
// default view rather than reaching go-api verbatim.
var DiscographyGroupings = []string{"artist", "provider", "contamination_band"}

// defaultDiscographyGrouping is the seam default go-api applies when by= is absent;
// an out-of-allowlist pivot falls back to it.
const defaultDiscographyGrouping = "artist"

func knownGrouping(by string) bool {
	for _, g := range DiscographyGroupings {
		if g == by {
			return true
		}
	}
	return false
}

// AdminDiscographyQualityBy fetches GET /admin/quality/discography?by=<grouping>,
// the same operator read as AdminDiscographyQuality but grouped on demand for the
// owner's pivot. by is constrained to DiscographyGroupings; anything else falls
// back to the seam default, so an unbounded or hostile pivot value can never reach
// the endpoint. It goes through the same guarded transport as every other read —
// the operator bearer and host pin come from the shared newRequest (only RawQuery
// is added; scheme and host stay fixed by the base), and the bounded body, the
// timeout and the single 401-refresh retry all apply. It is a pure read: nothing
// here writes, commands or re-runs any go-api pipeline.
func (c *Client) AdminDiscographyQualityBy(ctx context.Context, by string) (DiscographyQuality, error) {
	if !knownGrouping(by) {
		by = defaultDiscographyGrouping
	}
	query := url.Values{"by": {by}}
	var out DiscographyQuality
	if err := c.getWithQuery(ctx, adminDiscographyQualityPath, query, &out); err != nil {
		return DiscographyQuality{}, err
	}
	return out, nil
}

// getWithQuery is the query-carrying read the pivot needs: the shared get primitive
// joins path segments structurally and escapes a "?", so it cannot pass query
// params. This mirrors get's single-401-refresh-retry contract and builds on the
// same guarded newRequest (host pin + operator bearer) and the same bounded-body /
// error-mapping loop, adding only RawQuery — it never edits or weakens the shared
// client core, and stays observe-only (a GET, no write counterpart).
func (c *Client) getWithQuery(ctx context.Context, path string, query url.Values, out any) error {
	op := "GET " + path + "?" + query.Encode()
	err := c.getOnceWithQuery(ctx, op, path, query, out)
	if !c.shouldRefreshRetry(err) {
		return err
	}
	invalidateOn401(c.tokens, http.StatusUnauthorized)
	return c.getOnceWithQuery(ctx, op, path, query, out)
}

// getOnceWithQuery reuses newRequest for the host pin and operator bearer, then
// sets RawQuery on the already-host-pinned URL (RawQuery can never move the request
// off the configured host) and runs the identical bounded-body read-and-decode as
// getOnce. The transport plumbing is duplicated here rather than in client.go so
// the query capability is fully additive and the shared core stays untouched.
func (c *Client) getOnceWithQuery(ctx context.Context, op, path string, query url.Values, out any) error {
	req, err := c.newRequest(ctx, path)
	if err != nil {
		return err
	}
	req.URL.RawQuery = query.Encode()
	resp, err := c.http.Do(req)
	if err != nil {
		return &SourceDownError{Op: op, Err: err}
	}
	if resp == nil {
		return &SourceDownError{Op: op, Err: fmt.Errorf("nil response")}
	}
	defer func() { _, _ = io.CopyN(io.Discard, resp.Body, maxBodyBytes); _ = resp.Body.Close() }()

	body := io.LimitReader(resp.Body, maxBodyBytes)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return apiError(op, resp.StatusCode, body)
	}
	if err := json.NewDecoder(body).Decode(out); err != nil {
		return fmt.Errorf("goapi: %s: decode response: %w", op, err)
	}
	return nil
}
