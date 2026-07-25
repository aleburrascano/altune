package ports

import (
	"context"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
)

type AudioCandidate struct {
	Title      string
	Duration   float64
	URL        string
	Channel    string
	Categories []string
	ViewCount  int64
}

func DedupeCandidatesByURL(merged []AudioCandidate, results []AudioCandidate, seen map[string]bool) []AudioCandidate {
	for _, c := range results {
		if c.URL == "" || seen[c.URL] {
			continue
		}
		seen[c.URL] = true
		merged = append(merged, c)
	}
	return merged
}

type AudioSearcher interface {
	Search(ctx context.Context, query string) ([]AudioCandidate, error)
	Download(ctx context.Context, url string, outDir string) (filePath string, err error)
}

type TrackTags struct {
	Title       string
	Artist      string
	Album       string
	AlbumArtist string
	Genre       string
	Year        int
	TrackNumber int
}

type AudioTagger interface {
	Tag(ctx context.Context, filePath string, tags TrackTags) error
}

type AudioProber interface {
	ProbeDuration(ctx context.Context, filePath string) (float64, error)
	ValidateDecodable(ctx context.Context, filePath string) error
}

type AudioWriter interface {
	Exists(ctx context.Context, audioRef string) (bool, error)
	Store(ctx context.Context, sourcePath string, audioRef string) error
	Delete(ctx context.Context, audioRef string) error
}

type TrackRepository interface {
	GetByID(ctx context.Context, id domain.TrackId, userId shared.UserId) (*domain.Track, error)
	Update(ctx context.Context, track *domain.Track) error
}
