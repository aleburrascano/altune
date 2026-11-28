package savedlink

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/filelinks"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
)

const SourceName domain.SourceName = "catalogpin"

const pinnedProvider = domain.ProviderSoundCloud

type audioFetcher interface {
	Download(ctx context.Context, url string, outDir string) (domain.FetchedAudio, error)
	MarkUnplayable(ctx context.Context, candidates []domain.AudioCandidate)
}

var _ ports.AudioSource = (*Source)(nil)

type pageReader interface {
	ReadPage(ctx context.Context, pageURL string) ([]byte, error)
}

type Source struct {
	fetcher audioFetcher
	pages   pageReader
}

var (
	ErrFetcherRequired    = errors.New("savedlink: an audio fetcher is required")
	ErrPageReaderRequired = errors.New("savedlink: a page reader is required")
	ErrNoPageReader       = errors.New("savedlink: no page reader is wired")
)

func NewSource(fetcher audioFetcher, pages pageReader) (*Source, error) {
	if fetcher == nil {
		return nil, ErrFetcherRequired
	}
	if pages == nil {
		return nil, ErrPageReaderRequired
	}
	return &Source{fetcher: fetcher, pages: pages}, nil
}

func MustNewSource(fetcher audioFetcher, pages pageReader) *Source {
	s, err := NewSource(fetcher, pages)
	if err != nil {
		panic(err)
	}
	return s
}

type noPageReader struct{}

func NoPageReader() pageReader { return noPageReader{} }

func (noPageReader) ReadPage(context.Context, string) ([]byte, error) { return nil, ErrNoPageReader }

func (s *Source) Name() domain.SourceName { return SourceName }

func (s *Source) Find(ctx context.Context, req domain.FindRequest) ([]domain.AudioCandidate, error) {
	candidates := s.pinnedCandidates(ctx, req)
	s.fetcher.MarkUnplayable(ctx, candidates)
	return candidates, nil
}

func (s *Source) pinnedCandidates(ctx context.Context, req domain.FindRequest) []domain.AudioCandidate {
	if files := s.fileHostCandidates(ctx, req); len(files) > 0 {
		return files
	}
	return soundCloudCandidates(ctx, req)
}

func (s *Source) fileHostCandidates(ctx context.Context, req domain.FindRequest) []domain.AudioCandidate {
	var files []domain.AudioCandidate
	for _, pin := range req.CatalogSources {
		if !filelinks.IsProvider(pin.Provider) {
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

func (s *Source) fileURL(ctx context.Context, pin domain.CatalogSource) (string, bool) {
	if download, ok := filelinks.DownloadURL(pin.Provider, pin.ExternalID); ok {
		return download, true
	}
	page, ok := filelinks.FileOnPage(pin.Provider, pin.ExternalID)
	if !ok {
		return "", false
	}
	body, err := s.pages.ReadPage(ctx, page.PageURL)
	if err != nil {
		slog.WarnContext(ctx, "acquisition.catalogpin_page_unreadable", "provider", pin.Provider, logging.KeyError, err)
		return "", false
	}
	file := page.File.Find(body)
	return string(file), file != nil
}

func soundCloudCandidates(ctx context.Context, req domain.FindRequest) []domain.AudioCandidate {
	var pinned []domain.AudioCandidate
	for _, pin := range req.CatalogSources {
		if pin.Provider != string(pinnedProvider) || !usablePinURL(ctx, req, pin) {
			continue
		}
		pinned = append(pinned, pinnedCandidate(req, pin.URL))
	}
	if len(pinned) == 0 && isHTTPSURL(req.KnownSourceURL) {
		return []domain.AudioCandidate{pinnedCandidate(req, req.KnownSourceURL)}
	}
	return pinned
}

func usablePinURL(ctx context.Context, req domain.FindRequest, pin domain.CatalogSource) bool {
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

func pinnedCandidate(req domain.FindRequest, pinURL string) domain.AudioCandidate {
	return domain.AudioCandidate{
		URL:     pinURL,
		Title:   req.Title,
		Artists: []string{req.Artist},
		Kind:    domain.KindCatalogPin,
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

func (s *Source) Fetch(ctx context.Context, candidate domain.AudioCandidate, outDir string) (domain.FetchedAudio, error) {
	r0, err := s.fetcher.Download(ctx, candidate.URL, outDir)
	if err != nil {
		return r0, fmt.Errorf("download audio: %w", err)
	}
	return r0, nil
}
