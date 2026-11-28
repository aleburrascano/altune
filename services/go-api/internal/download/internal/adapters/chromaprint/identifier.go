package chromaprint

import (
	"altune/go-api/internal/download/internal/adapters/ytdlp"
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/platform/outbound"
	"altune/go-api/internal/shared/childprocess"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	ErrMissingAPIKey  = errors.New("chromaprint: AcoustID API key is empty")
	ErrClientRequired = errors.New("chromaprint: an HTTP client is required")
)

const (
	fingerprintTimeout = 60 * time.Second
	lookupTimeout      = 15 * time.Second
	MinScore           = 0.85
	defaultEndpoint    = "https://api.acoustid.org/v2/lookup"
	lookupBodyCap      = 2 << 20
)

var _ ports.AudioIdentifier = (*Identifier)(nil)

type Identifier struct {
	fpcalc   string
	apiKey   string
	endpoint string
	client   *http.Client
}

func WithEndpoint(endpoint string) func(*Identifier) {
	return func(i *Identifier) { i.endpoint = endpoint }
}

func NewIdentifier(ctx context.Context, binDir, apiKey string, client *http.Client, opts ...func(*Identifier)) (*Identifier, error) {
	if client == nil {
		return nil, ErrClientRequired
	}
	key := strings.TrimSpace(apiKey)
	if key == "" {
		slog.WarnContext(ctx, "chromaprint: AcoustID API key is empty or blank; "+
			"audio fingerprint identification is disabled and every lookup will fail")
	}
	bounded := *client
	bounded.Timeout = lookupTimeout
	id := &Identifier{
		fpcalc:   ytdlp.ResolveBinary("fpcalc", binDir),
		apiKey:   key,
		endpoint: defaultEndpoint,
		client:   &bounded,
	}
	for _, opt := range opts {
		opt(id)
	}
	return id, nil
}

func MustNewIdentifier(ctx context.Context, binDir, apiKey string, client *http.Client, opts ...func(*Identifier)) *Identifier {
	i, err := NewIdentifier(ctx, binDir, apiKey, client, opts...)
	if err != nil {
		panic(err)
	}
	return i
}

func (i *Identifier) Available() bool {
	if i.apiKey == "" {
		return false
	}
	return ytdlp.BinaryRunnable(i.fpcalc)
}

type fingerprint struct {
	Duration    float64 `json:"duration"`
	Fingerprint string  `json:"fingerprint"`
}

func (i *Identifier) Identify(ctx context.Context, filePath string, fullLengthForPreview float64) (domain.RecordingMatch, error) {
	fp, err := i.fingerprintFile(ctx, filePath)
	if err != nil {
		return domain.RecordingMatch{}, err
	}
	if fp.Fingerprint == "" || fp.Duration <= 0 {
		return domain.RecordingMatch{}, fmt.Errorf("%s: %w", filePath, domain.ErrEmptyFingerprint)
	}
	if fullLengthForPreview > 0 {
		fp.Duration = math.Round(fullLengthForPreview)
	}
	return i.lookup(ctx, fp)
}

func (i *Identifier) fingerprintFile(ctx context.Context, filePath string) (fingerprint, error) {
	stdout, stderr, err := childprocess.RunWithTimeout(ctx, fingerprintTimeout, i.fpcalc, "-json", filePath)
	if err != nil {
		return fingerprint{}, fmt.Errorf("fpcalc: %w (stderr: %s)", err, stderr)
	}

	var fp fingerprint
	if err := json.Unmarshal([]byte(stdout), &fp); err != nil {
		return fingerprint{}, fmt.Errorf("parse fpcalc output: %w", err)
	}
	return fp, nil
}

type lookupRecording struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Duration float64 `json:"duration"`
	Artists  []struct {
		Name string `json:"name"`
	} `json:"artists"`
	ReleaseGroups []struct {
		ID             string   `json:"id"`
		Title          string   `json:"title"`
		Type           string   `json:"type"`
		SecondaryTypes []string `json:"secondarytypes"`
	} `json:"releasegroups"`
}

type lookupResponse struct {
	Status string `json:"status"`
	Error  struct {
		Message string `json:"message"`
	} `json:"error"`
	Results []struct {
		ID         string            `json:"id"`
		Score      float64           `json:"score"`
		Recordings []lookupRecording `json:"recordings"`
	} `json:"results"`
}

func (i *Identifier) lookup(ctx context.Context, fp fingerprint) (domain.RecordingMatch, error) {
	if i.apiKey == "" {
		return domain.RecordingMatch{}, ErrMissingAPIKey
	}

	form := url.Values{}
	form.Set("client", i.apiKey)
	form.Set("meta", "recordings releasegroups")
	form.Set("duration", strconv.Itoa(int(fp.Duration)))
	form.Set("fingerprint", fp.Fingerprint)

	status, body, err := outbound.PostBytesCapped(ctx, i.client, i.endpoint, strings.NewReader(form.Encode()), lookupBodyCap+1,
		outbound.WithHeader("Content-Type", "application/x-www-form-urlencoded"))
	switch {
	case err != nil:
		return domain.RecordingMatch{}, fmt.Errorf("acoustid lookup: %w", err)
	case status == http.StatusTooManyRequests:
		return domain.RecordingMatch{}, fmt.Errorf("acoustid lookup: status %d: %w", status, domain.ErrIdentifyThrottled)
	case status != http.StatusOK:
		return domain.RecordingMatch{}, fmt.Errorf("acoustid lookup: status %d", status)
	}
	if len(body) >= lookupBodyCap {
		return domain.RecordingMatch{}, fmt.Errorf("read acoustid response: exceeds %d byte cap", lookupBodyCap)
	}

	var parsed lookupResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return domain.RecordingMatch{}, fmt.Errorf("parse acoustid response: %w", err)
	}
	if parsed.Status != "ok" {
		return domain.RecordingMatch{}, fmt.Errorf("acoustid lookup: %s", parsed.Error.Message)
	}

	return matchOf(acoustIDResults(parsed)), nil
}

func matchOf(results []domain.AcoustIDResult) domain.RecordingMatch {
	for _, result := range results {
		mbids := make([]string, 0, len(result.Recordings))
		for _, rec := range result.Recordings {
			if rec.MBID != "" {
				mbids = append(mbids, rec.MBID)
			}
		}
		if result.ID == "" && len(mbids) == 0 {
			continue
		}
		return domain.RecordingMatch{AcoustID: result.ID, MBIDs: mbids, Score: result.Score, Results: results}
	}
	return domain.RecordingMatch{Results: results}
}

func acoustIDResults(full lookupResponse) []domain.AcoustIDResult {
	results := make([]domain.AcoustIDResult, 0, len(full.Results))
	for _, result := range full.Results {
		if result.Score < MinScore {
			continue
		}
		results = append(results, domain.AcoustIDResult{
			ID:         result.ID,
			Score:      result.Score,
			Recordings: linkedRecordings(result.Recordings),
		})
	}
	sort.SliceStable(results, func(a, b int) bool { return results[a].Score > results[b].Score })
	return results
}

func linkedRecordings(recordings []lookupRecording) []domain.LinkedRecording {
	linked := make([]domain.LinkedRecording, 0, len(recordings))
	for _, recording := range recordings {
		linked = append(linked, domain.LinkedRecording{
			MBID:          recording.ID,
			Title:         recording.Title,
			Artists:       artistNames(recording.Artists),
			Duration:      recording.Duration,
			ReleaseGroups: releaseGroups(recording),
		})
	}
	return linked
}

func artistNames(artists []struct {
	Name string `json:"name"`
},
) []string {
	names := make([]string, 0, len(artists))
	for _, artist := range artists {
		if artist.Name != "" {
			names = append(names, artist.Name)
		}
	}
	return names
}

func releaseGroups(recording lookupRecording) []domain.ReleaseGroup {
	if len(recording.ReleaseGroups) == 0 {
		return nil
	}
	groups := make([]domain.ReleaseGroup, 0, len(recording.ReleaseGroups))
	for _, group := range recording.ReleaseGroups {
		groups = append(groups, domain.ReleaseGroup{
			MBID:           group.ID,
			Title:          group.Title,
			Type:           group.Type,
			SecondaryTypes: group.SecondaryTypes,
		})
	}
	return groups
}
