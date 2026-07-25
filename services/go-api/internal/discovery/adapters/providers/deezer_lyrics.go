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
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"

	"golang.org/x/sync/singleflight"
)

const (
	deezerAuthAnonymousURL = "https://auth.deezer.com/login/anonymous?jo=p&rto=c&i=c"
	deezerPipeURL          = "https://pipe.deezer.com/api"
	deezerLyricsMaxBody    = 1 << 20
)

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

type DeezerLyricsAdapter struct {
	resolver *DeezerAdapter
	jwt      *deezerJWTResolver
	client   *http.Client
}

func NewDeezerLyricsAdapter(client *http.Client) *DeezerLyricsAdapter {
	return &DeezerLyricsAdapter{
		resolver: NewDeezerAdapter(client),
		jwt:      newDeezerJWTResolver(client),
		client:   client,
	}
}

func (a *DeezerLyricsAdapter) ResolveTrackID(ctx context.Context, artist, title string) (string, error) {
	return a.resolver.ResolveID(ctx, domain.ResultKindTrack, artist, title)
}

func (a *DeezerLyricsAdapter) Lookup(ctx context.Context, trackID string) (domain.DeezerLyrics, error) {
	if strings.TrimSpace(trackID) == "" {
		return domain.EmptyDeezerLyrics(), nil
	}

	jwt, err := a.jwt.get(ctx)
	if err != nil {
		return domain.EmptyDeezerLyrics(), fmt.Errorf("deezer anonymous jwt: %w", err)
	}
	body, status, err := a.postLyrics(ctx, jwt, trackID)
	if err != nil {
		return domain.EmptyDeezerLyrics(), err
	}
	if status == http.StatusUnauthorized {
		a.jwt.invalidate(jwt)
		jwt, err = a.jwt.get(ctx)
		if err != nil {
			return domain.EmptyDeezerLyrics(), fmt.Errorf("deezer anonymous jwt: %w", err)
		}
		body, status, err = a.postLyrics(ctx, jwt, trackID)
		if err != nil {
			return domain.EmptyDeezerLyrics(), err
		}
	}
	if status != http.StatusOK {
		return domain.EmptyDeezerLyrics(), fmt.Errorf("deezer pipe lyrics returned %d", status)
	}

	return parseSynchronizedLyrics(body)
}

func (a *DeezerLyricsAdapter) postLyrics(ctx context.Context, jwt, trackID string) ([]byte, int, error) {
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

func parseSynchronizedLyrics(body []byte) (domain.DeezerLyrics, error) {
	var env struct {
		Data struct {
			Track struct {
				Lyrics *struct {
					Copyright         string `json:"copyright"`
					Text              string `json:"text"`
					Writers           string `json:"writers"`
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

func splitDeezerWriters(s string) []string {
	out := []string{}
	for _, w := range strings.Split(s, ",") {
		if w = strings.TrimSpace(w); w != "" {
			out = append(out, w)
		}
	}
	return out
}

type deezerJWTResolver struct {
	client  *http.Client
	authURL string
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
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), deezerJWTResolveTimeout)
		defer cancel()
		return r.resolve(rctx)
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

const deezerJWTResolveTimeout = 10 * time.Second

func (r *deezerJWTResolver) invalidate(failed string) {
	r.mu.Lock()
	if r.cached == failed {
		r.cached = ""
	}
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

	var out struct {
		JWT string `json:"jwt"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, deezerLyricsMaxBody)).Decode(&out); err != nil {
		return "", fmt.Errorf("decode deezer anonymous jwt: %w", err)
	}
	return strings.TrimSpace(out.JWT), nil
}
