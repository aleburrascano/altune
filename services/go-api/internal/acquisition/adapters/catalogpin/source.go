package catalogpin

import (
	acqdomain "altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/acquisition/ports"
	"context"
	"log/slog"
	"net/url"
	"strings"
)

const SourceName acqdomain.SourceName = "catalogpin"

const pinnedProvider = ports.ProviderSoundCloud

type audioFetcher interface {
	Download(ctx context.Context, url string, outDir string) (ports.FetchedAudio, error)
}

var _ ports.AudioSource = (*Source)(nil)

type Source struct {
	fetcher audioFetcher
}

func NewSource(fetcher audioFetcher) *Source {
	return &Source{fetcher: fetcher}
}

func (s *Source) Name() acqdomain.SourceName { return SourceName }

func (s *Source) Find(ctx context.Context, req ports.FindRequest) ([]ports.AudioCandidate, error) {
	var pinned []ports.AudioCandidate
	for _, pin := range req.CatalogSources {
		if pin.Provider != string(pinnedProvider) {
			continue
		}
		if pin.URL == "" {
			slog.DebugContext(ctx, "acquisition.catalogpin_url_missing",
				"external_id", pin.ExternalID, "title", req.Title)
			continue
		}
		if !isSoundCloudURL(pin.URL) {
			slog.WarnContext(ctx, "acquisition.catalogpin_url_rejected",
				"url", pin.URL, "title", req.Title)
			continue
		}
		pinned = append(pinned, pinnedCandidate(req, pin.URL))
	}
	if len(pinned) == 0 && isHTTPSURL(req.KnownSourceURL) {
		return []ports.AudioCandidate{pinnedCandidate(req, req.KnownSourceURL)}, nil
	}
	return pinned, nil
}

func pinnedCandidate(req ports.FindRequest, pinURL string) ports.AudioCandidate {
	return ports.AudioCandidate{
		URL:      pinURL,
		Title:    req.Title,
		Artists:  []string{req.Artist},
		Resolved: true,
		Kind:     ports.KindCatalogPin,
	}
}

func httpsHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u == nil || u.Scheme != "https" {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func isHTTPSURL(rawURL string) bool {
	return httpsHost(rawURL) != ""
}

func isSoundCloudURL(rawURL string) bool {
	host := httpsHost(rawURL)
	return host == "soundcloud.com" || strings.HasSuffix(host, ".soundcloud.com")
}

func (s *Source) Fetch(ctx context.Context, candidate ports.AudioCandidate, outDir string) (ports.FetchedAudio, error) {
	return s.fetcher.Download(ctx, candidate.URL, outDir)
}
