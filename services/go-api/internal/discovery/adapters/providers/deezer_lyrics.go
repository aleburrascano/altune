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
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
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

	status, body, err := postBytesCapped(ctx, a.client, deezerPipeURL, bytes.NewReader(payload), deezerLyricsMaxBody,
		withHeader("Content-Type", "application/json"),
		withHeader("Authorization", "Bearer "+jwt))
	return body, status, err
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
	*cachedResolver[string]
	client  *http.Client
	authURL string
}

func newDeezerJWTResolver(client *http.Client) *deezerJWTResolver {
	r := &deezerJWTResolver{client: client, authURL: deezerAuthAnonymousURL}
	r.cachedResolver = newCachedResolver("anon_jwt", deezerJWTResolveTimeout, r.resolve, nonEmpty)
	return r
}

const deezerJWTResolveTimeout = 10 * time.Second

func (r *deezerJWTResolver) resolve(ctx context.Context) (string, time.Time, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.authURL, nil)
	if err != nil {
		return "", time.Time{}, err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return "", time.Time{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", time.Time{}, fmt.Errorf("deezer anonymous auth returned %d", resp.StatusCode)
	}

	var out struct {
		JWT string `json:"jwt"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, deezerLyricsMaxBody)).Decode(&out); err != nil {
		return "", time.Time{}, fmt.Errorf("decode deezer anonymous jwt: %w", err)
	}
	jwt := strings.TrimSpace(out.JWT)
	if jwt == "" {
		return "", time.Time{}, errors.New("deezer: resolved empty anonymous jwt")
	}
	return jwt, time.Time{}, nil
}
