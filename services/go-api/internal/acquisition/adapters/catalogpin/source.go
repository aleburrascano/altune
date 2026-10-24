package catalogpin

import (
	acqdomain "altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared/filehost"
	"context"
	"log/slog"
	"net/url"
	"strings"
)

const SourceName acqdomain.SourceName = "catalogpin"

const pinnedProvider = ports.ProviderSoundCloud

type audioFetcher interface {
	Download(ctx context.Context, url string, outDir string) (ports.FetchedAudio, error)
	MarkUnplayable(ctx context.Context, candidates []ports.AudioCandidate)
}

var _ ports.AudioSource = (*Source)(nil)

type pageReader interface {
	ReadPage(ctx context.Context, pageURL string) ([]byte, error)
}

type Source struct {
	fetcher audioFetcher
	pages   pageReader
}

type Option func(*Source)

func WithPageReader(pages pageReader) Option {
	return func(s *Source) { s.pages = pages }
}

func NewSource(fetcher audioFetcher, opts ...Option) *Source {
	s := &Source{fetcher: fetcher}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func (s *Source) Name() acqdomain.SourceName { return SourceName }

func (s *Source) Find(ctx context.Context, req ports.FindRequest) ([]ports.AudioCandidate, error) {
	candidates := s.pinnedCandidates(ctx, req)
	s.fetcher.MarkUnplayable(ctx, candidates)
	return candidates, nil
}

func (s *Source) pinnedCandidates(ctx context.Context, req ports.FindRequest) []ports.AudioCandidate {
	if files := s.fileHostCandidates(ctx, req); len(files) > 0 {
		return files
	}
	return soundCloudCandidates(ctx, req)
}

func (s *Source) fileHostCandidates(ctx context.Context, req ports.FindRequest) []ports.AudioCandidate {
	var files []ports.AudioCandidate
	for _, pin := range req.CatalogSources {
		if !filehost.IsProvider(pin.Provider) {
			continue
		}
		download, ok := s.fileURL(ctx, pin)
		if !ok {
			slog.WarnContext(ctx, "acquisition.catalogpin_file_id_rejected", "provider", pin.Provider, "title", req.Title)
			continue
		}
		files = append(files, pinnedCandidate(req, download))
	}
	return files
}

func (s *Source) fileURL(ctx context.Context, pin ports.CatalogSource) (string, bool) {
	if download, ok := filehost.DownloadURL(pin.Provider, pin.ExternalID); ok {
		return download, true
	}
	page, ok := filehost.FileOnPage(pin.Provider, pin.ExternalID)
	if !ok || s.pages == nil {
		return "", false
	}
	body, err := s.pages.ReadPage(ctx, page.PageURL)
	if err != nil {
		slog.WarnContext(ctx, "acquisition.catalogpin_page_unreadable", "provider", pin.Provider, "error", err)
		return "", false
	}
	file := page.File.Find(body)
	return string(file), file != nil
}

func soundCloudCandidates(ctx context.Context, req ports.FindRequest) []ports.AudioCandidate {
	var pinned []ports.AudioCandidate
	for _, pin := range req.CatalogSources {
		if pin.Provider != string(pinnedProvider) || !usablePinURL(ctx, req, pin) {
			continue
		}
		pinned = append(pinned, pinnedCandidate(req, pin.URL))
	}
	if len(pinned) == 0 && isHTTPSURL(req.KnownSourceURL) {
		return []ports.AudioCandidate{pinnedCandidate(req, req.KnownSourceURL)}
	}
	return pinned
}

func usablePinURL(ctx context.Context, req ports.FindRequest, pin ports.CatalogSource) bool {
	if pin.URL == "" {
		slog.DebugContext(ctx, "acquisition.catalogpin_url_missing",
			"external_id", pin.ExternalID, "title", req.Title)
		return false
	}
	if !isSoundCloudURL(pin.URL) {
		slog.WarnContext(ctx, "acquisition.catalogpin_url_rejected",
			"url", pin.URL, "title", req.Title)
		return false
	}
	return true
}

func pinnedCandidate(req ports.FindRequest, pinURL string) ports.AudioCandidate {
	return ports.AudioCandidate{
		URL:     pinURL,
		Title:   req.Title,
		Artists: []string{req.Artist},
		Kind:    ports.KindCatalogPin,
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
