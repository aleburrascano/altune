package eval

import (
	"context"
	"fmt"
	"strings"

	"altune/go-api/internal/shared/textnorm"
)

type DetailGolden struct {
	Name              string            `json:"name"`
	MBID              string            `json:"mbid"`
	SeedProvider      string            `json:"seed_provider"`
	SeedID            string            `json:"seed_id"`
	Identity          map[string]string `json:"identity"` // provider -> id; may be deliberately fractured
	ExpectedAlbums    []string          `json:"expected_albums"`
	ExpectedTopTracks []string          `json:"expected_top_tracks"`
	ForbiddenSources  []string          `json:"forbidden_sources"` // provider names that must not source any result
	ForbiddenTitles   []string          `json:"forbidden_titles"`  // titles that must not appear (other-artist markers)
}

type DetailItem struct {
	Title      string
	Sources    []string
	HasArtwork bool
	Year       int
}

type DetailService interface {
	Albums(ctx context.Context, seedProvider, seedID, artistName string) []DetailItem
	TopTracks(ctx context.Context, seedProvider, seedID, artistName string) []DetailItem
}

type DetailReport struct {
	Goldens            int             `json:"goldens"`
	ContaminationCount int             `json:"contamination_count"`
	AlbumRecall        float64         `json:"album_recall"`
	TrackRecall        float64         `json:"track_recall"`
	MetadataCoverage   float64         `json:"metadata_coverage"`
	PerArtist          []DetailArtist  `json:"per_artist"`
	Fails              []FailureRecord `json:"failures"`
}

type DetailArtist struct {
	Name             string  `json:"name"`
	Albums           int     `json:"albums"`
	Tracks           int     `json:"tracks"`
	Contamination    int     `json:"contamination"`
	AlbumRecall      float64 `json:"album_recall"`
	TrackRecall      float64 `json:"track_recall"`
	MetadataCoverage float64 `json:"metadata_coverage"`
}

func (r DetailReport) Metrics() []NamedMetric {
	return []NamedMetric{
		{Name: "detail.contamination", Value: float64(r.ContaminationCount), HigherIsBetter: false},
		{Name: "detail.album_recall", Value: r.AlbumRecall, HigherIsBetter: true},
		{Name: "detail.track_recall", Value: r.TrackRecall, HigherIsBetter: true},
		{Name: "detail.metadata_coverage", Value: r.MetadataCoverage, HigherIsBetter: true},
	}
}

func (r DetailReport) Failures() []FailureRecord { return r.Fails }

func RunDetailEval(ctx context.Context, goldens []DetailGolden, svc DetailService) DetailReport {
	rep := DetailReport{Goldens: len(goldens)}
	var albumRecallSum, trackRecallSum, coverageSum float64
	var albumRecallN, trackRecallN, coverageArtists int

	for _, g := range goldens {
		albums := svc.Albums(ctx, g.SeedProvider, g.SeedID, g.Name)
		tracks := svc.TopTracks(ctx, g.SeedProvider, g.SeedID, g.Name)

		contam := countContamination(g, albums, tracks, &rep.Fails)
		albumRecall := recall(g.ExpectedAlbums, albums)
		trackRecall := recall(g.ExpectedTopTracks, tracks)
		coverage, hasAlbums := metadataCoverage(albums)

		rep.ContaminationCount += contam
		if len(g.ExpectedAlbums) > 0 {
			albumRecallSum += albumRecall
			albumRecallN++
			missingRecall(g, "album", g.ExpectedAlbums, albums, albumRecall, &rep.Fails)
		}
		if len(g.ExpectedTopTracks) > 0 {
			trackRecallSum += trackRecall
			trackRecallN++
			missingRecall(g, "track", g.ExpectedTopTracks, tracks, trackRecall, &rep.Fails)
		}
		if hasAlbums {
			coverageSum += coverage
			coverageArtists++
		}

		rep.PerArtist = append(rep.PerArtist, DetailArtist{
			Name: g.Name, Albums: len(albums), Tracks: len(tracks),
			Contamination: contam, AlbumRecall: albumRecall,
			TrackRecall: trackRecall, MetadataCoverage: coverage,
		})
	}

	if albumRecallN > 0 {
		rep.AlbumRecall = albumRecallSum / float64(albumRecallN)
	}
	if trackRecallN > 0 {
		rep.TrackRecall = trackRecallSum / float64(trackRecallN)
	}
	if coverageArtists > 0 {
		rep.MetadataCoverage = coverageSum / float64(coverageArtists)
	}
	return rep
}

func countContamination(g DetailGolden, albums, tracks []DetailItem, fails *[]FailureRecord) int {
	forbSrc := stringSet(g.ForbiddenSources)
	forbTitle := normSet(g.ForbiddenTitles)
	count := 0
	for kind, items := range map[string][]DetailItem{"album": albums, "track": tracks} {
		for _, it := range items {
			bad := ""
			for _, s := range it.Sources {
				if forbSrc[s] {
					bad = "source=" + s
					break
				}
			}
			if bad == "" && forbTitle[textnorm.NormalizeForMatch(it.Title)] {
				bad = "forbidden-title"
			}
			if bad != "" {
				count++
				*fails = append(*fails, FailureRecord{
					Query:  g.Name,
					Reason: fmt.Sprintf("contamination (%s): %q [%s]", kind, it.Title, bad),
					Attrs:  map[string]any{"artist": g.Name, "kind": kind},
				})
			}
		}
	}
	return count
}

func recall(expected []string, items []DetailItem) float64 {
	if len(expected) == 0 {
		return 1
	}
	found := 0
	for _, want := range expected {
		if titlePresent(want, items) {
			found++
		}
	}
	return float64(found) / float64(len(expected))
}

func titlePresent(want string, items []DetailItem) bool {
	wantNorm := textnorm.NormalizeForMatch(want)
	if wantNorm == "" {
		return true
	}
	for _, it := range items {
		if strings.Contains(textnorm.NormalizeForMatch(it.Title), wantNorm) {
			return true
		}
	}
	return false
}

func missingRecall(g DetailGolden, kind string, expected []string, items []DetailItem, r float64, fails *[]FailureRecord) {
	if r >= 1 {
		return
	}
	for _, want := range expected {
		if !titlePresent(want, items) {
			*fails = append(*fails, FailureRecord{
				Query:  g.Name,
				Reason: fmt.Sprintf("missing %s: %q", kind, want),
				Attrs:  map[string]any{"artist": g.Name, "kind": kind},
			})
		}
	}
}

func metadataCoverage(albums []DetailItem) (float64, bool) {
	if len(albums) == 0 {
		return 0, false
	}
	complete := 0
	for _, a := range albums {
		if a.HasArtwork && a.Year > 0 {
			complete++
		}
	}
	return float64(complete) / float64(len(albums)), true
}

func stringSet(xs []string) map[string]bool {
	s := make(map[string]bool, len(xs))
	for _, x := range xs {
		s[x] = true
	}
	return s
}

func normSet(xs []string) map[string]bool {
	s := make(map[string]bool, len(xs))
	for _, x := range xs {
		if k := textnorm.NormalizeForMatch(x); k != "" {
			s[k] = true
		}
	}
	return s
}
