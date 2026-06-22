package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"

	"golang.org/x/sync/singleflight"
)

// AIDEV-NOTE: Deezer lyrics (docs/providers/deezer.md cap 6 — the headline).
// Lyrics are the one metadata axis no other audited provider carries. The public
// API does not expose them and the legacy gw-light song.getLyrics is auth-gated;
// the working anonymous path is the internal pipe.deezer.com GraphQL, reached with
// an anonymous JWT bootstrapped from auth.deezer.com. ToS: reverse-engineered,
// against ToS — accepted for self-hosted personal/family use (README doctrine),
// same posture as SoundCloud's client_id path.
//
// Track-id resolution reuses the public-API DeezerAdapter search (no second
// search implementation). Endpoint shapes are from the live probe documented in
// deezer.md §4; the auth-response field name is [INFERRED] (the JWT body was not
// field-dumped this session) — corrected on the next live probe if wrong.

// AIDEV-WARNING: pipe.deezer.com + auth.deezer.com are reverse-engineered internal
// endpoints. Keep this path display-only and self-healing on the anonymous-JWT
// rotation (401 → re-bootstrap once). Never block the hot search path on it.

const (
	deezerAuthAnonymousURL = "https://auth.deezer.com/login/anonymous?jo=p&rto=c&i=c"
	deezerPipeURL          = "https://pipe.deezer.com/api"
	deezerLyricsMaxBody    = 1 << 20 // 1MB ceiling on a lyrics response
)

// synchronizedLyricsQuery is the verified pipe GraphQL query (deezer.md §4).
const synchronizedLyricsQuery = `query SynchronizedLyrics($trackId: String!) {
  track(trackId: $trackId) {
    id
    lyrics {
      id
      copyright
      text
      writers
      synchronizedLines { lrcTimestamp line milliseconds duration }
    }
  }
}`

var _ ports.LyricsProvider = (*DeezerLyricsAdapter)(nil)

// DeezerLyricsAdapter fetches a track's lyrics from Deezer. It delegates track-id
// resolution to the public-API DeezerAdapter and fetches the lyrics themselves
// from the internal pipe.deezer.com GraphQL, gated by a self-healing anonymous
// JWT.
type DeezerLyricsAdapter struct {
	resolver *DeezerAdapter
	jwt      *deezerJWTResolver
	client   *http.Client
}

// NewDeezerLyricsAdapter wires the lyrics adapter. The same http.Client is used
// for the public-API resolution, the anonymous-JWT bootstrap, and the pipe POST.
func NewDeezerLyricsAdapter(client *http.Client) *DeezerLyricsAdapter {
	return &DeezerLyricsAdapter{
		resolver: NewDeezerAdapter(client),
		jwt:      newDeezerJWTResolver(client),
		client:   client,
	}
}

// ResolveTrackID maps an (artist, title) to the top-matching Deezer track id via
// the public-API search, "" when nothing matches.
func (a *DeezerLyricsAdapter) ResolveTrackID(ctx context.Context, artist, title string) (string, error) {
	return a.resolver.ResolveID(ctx, domain.ResultKindTrack, artist, title)
}

// Lookup fetches the lyrics for a known track id. A definitive "no lyrics"
// (the track has none, or none for this region) returns an empty value + nil
// error so the service can negative-cache it; an auth/network/decode failure
// returns an error so the service degrades without poisoning the cache.
func (a *DeezerLyricsAdapter) Lookup(ctx context.Context, trackID string) (domain.DeezerLyrics, error) {
	if strings.TrimSpace(trackID) == "" {
		return domain.EmptyDeezerLyrics(), nil
	}

	body, status, err := a.postLyrics(ctx, trackID)
	if err != nil {
		return domain.EmptyDeezerLyrics(), err
	}
	// One self-heal on an expired/rotated anonymous JWT.
	if status == http.StatusUnauthorized {
		a.jwt.invalidate()
		body, status, err = a.postLyrics(ctx, trackID)
		if err != nil {
			return domain.EmptyDeezerLyrics(), err
		}
	}
	if status != http.StatusOK {
		return domain.EmptyDeezerLyrics(), fmt.Errorf("deezer pipe lyrics returned %d", status)
	}

	return parseSynchronizedLyrics(body)
}

// postLyrics performs one authenticated SynchronizedLyrics POST and returns the
// raw body + status. Bootstrap/transport failures are errors; an HTTP status
// (incl. 401) is returned to the caller to decide on self-heal.
func (a *DeezerLyricsAdapter) postLyrics(ctx context.Context, trackID string) ([]byte, int, error) {
	jwt, err := a.jwt.get(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("deezer anonymous jwt: %w", err)
	}

	payload, err := json.Marshal(map[string]any{
		"operationName": "SynchronizedLyrics",
		"query":         synchronizedLyricsQuery,
		"variables":     map[string]string{"trackId": trackID},
	})
	if err != nil {
		return nil, 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, deezerPipeURL, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+jwt)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, deezerLyricsMaxBody))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

// parseSynchronizedLyrics maps the pipe GraphQL response into the domain value
// object. A null lyrics object or a GraphQL error array is a definitive miss
// (empty + nil — negative-cacheable), not a failure.
func parseSynchronizedLyrics(body []byte) (domain.DeezerLyrics, error) {
	var env struct {
		Data struct {
			Track struct {
				Lyrics *struct {
					Copyright        string `json:"copyright"`
					Text             string `json:"text"`
					Writers          string `json:"writers"`
					SynchronizedLines []struct {
						LRCTimestamp string `json:"lrcTimestamp"`
						Line         string `json:"line"`
						Milliseconds int64  `json:"milliseconds"`
						Duration     int64  `json:"duration"`
					} `json:"synchronizedLines"`
				} `json:"lyrics"`
			} `json:"track"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return domain.EmptyDeezerLyrics(), fmt.Errorf("decode deezer lyrics: %w", err)
	}

	// LyricsNotFoundError (and friends) come back as a structured GraphQL error
	// with no usable data — a definitive miss, not a transient failure.
	if env.Data.Track.Lyrics == nil {
		return domain.EmptyDeezerLyrics(), nil
	}

	src := env.Data.Track.Lyrics
	out := domain.EmptyDeezerLyrics()
	out.Plain = strings.TrimSpace(src.Text)
	out.Copyright = strings.TrimSpace(src.Copyright)
	out.Writers = splitDeezerWriters(src.Writers)

	lines := make([]domain.SyncedLyricLine, 0, len(src.SynchronizedLines))
	for _, l := range src.SynchronizedLines {
		line := strings.TrimSpace(l.Line)
		ts := strings.TrimSpace(l.LRCTimestamp)
		// Deezer emits an empty trailing line as a synced-line separator; skip it.
		if line == "" && ts == "" {
			continue
		}
		lines = append(lines, domain.SyncedLyricLine{
			Timecode:     ts,
			Line:         line,
			Milliseconds: l.Milliseconds,
			Duration:     l.Duration,
		})
	}
	out.SyncedLines = lines
	return out, nil
}

// splitDeezerWriters turns Deezer's comma-joined writer credits into a trimmed,
// non-empty list. [INFERRED] writers is a scalar string in the probe ("A, B");
// if a future probe shows a JSON array this needs a custom unmarshal.
func splitDeezerWriters(s string) []string {
	out := []string{}
	for _, w := range strings.Split(s, ",") {
		if w = strings.TrimSpace(w); w != "" {
			out = append(out, w)
		}
	}
	return out
}

// deezerJWTResolver bootstraps and caches the anonymous JWT the pipe GraphQL
// backend requires. The cached token is reused until a 401 invalidates it (the
// rotation tax — lighter than SoundCloud's client_id: one GET, no JS scraping).
// Concurrent cold-start callers are collapsed with singleflight.
type deezerJWTResolver struct {
	client  *http.Client
	authURL string // overridable in tests
	sf      singleflight.Group
	mu      sync.Mutex
	cached  string
}

func newDeezerJWTResolver(client *http.Client) *deezerJWTResolver {
	return &deezerJWTResolver{client: client, authURL: deezerAuthAnonymousURL}
}

func (r *deezerJWTResolver) get(ctx context.Context) (string, error) {
	r.mu.Lock()
	cached := r.cached
	r.mu.Unlock()
	if cached != "" {
		return cached, nil
	}

	v, err, _ := r.sf.Do("anon_jwt", func() (any, error) {
		r.mu.Lock()
		existing := r.cached
		r.mu.Unlock()
		if existing != "" {
			return existing, nil
		}
		return r.resolve(ctx)
	})
	if err != nil {
		return "", err
	}

	jwt, _ := v.(string)
	if jwt == "" {
		return "", errors.New("deezer: resolved empty anonymous jwt")
	}
	r.mu.Lock()
	r.cached = jwt
	r.mu.Unlock()
	return jwt, nil
}

func (r *deezerJWTResolver) invalidate() {
	r.mu.Lock()
	r.cached = ""
	r.mu.Unlock()
}

func (r *deezerJWTResolver) resolve(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.authURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("deezer anonymous auth returned %d", resp.StatusCode)
	}

	// [INFERRED] response shape: {"jwt":"<token>"}. The JWT body was not field-
	// dumped in the audit session — confirm the field name on the next live probe.
	var out struct {
		JWT string `json:"jwt"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, deezerLyricsMaxBody)).Decode(&out); err != nil {
		return "", fmt.Errorf("decode deezer anonymous jwt: %w", err)
	}
	return strings.TrimSpace(out.JWT), nil
}
