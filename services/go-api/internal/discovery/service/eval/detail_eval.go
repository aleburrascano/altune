package eval

// Detail harness — the offline quality gate for the artist detail/discography
// path (okf/backend/discovery/artist-detail.md), the counterpart to the ranking
// eval for the OTHER discovery pipeline. It feeds each golden artist through the
// real detail service and scores three things the manual token+curl loop used to
// check by eye:
//
//   - contamination: a golden may carry a deliberately FRACTURED identity (a
//     wrong streaming edge fusing two same-name artists — the "Che" bug). Its
//     ForbiddenSources / ForbiddenTitles are the other artist's fingerprints;
//     ANY that survive into the output is same-name contamination the read-time
//     guards (MB anchor for albums, cohesion for top-tracks) failed to drop.
//   - recall: the golden's real releases/tracks are actually returned.
//   - metadata coverage: albums come back with artwork + year (a complete card).

import (
	"context"
	"fmt"
	"strings"

	"altune/go-api/internal/shared/textnorm"
)

// DetailGolden is one corpus entry: a known artist, the (possibly fractured)
// cross-provider identity to feed, and the expected/forbidden fingerprints.
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

// DetailItem is one returned album or track, projected to just the fields the
// harness scores (the cmd adapts the real ContentFetchResponse into these).
type DetailItem struct {
	Title      string
	Sources    []string
	HasArtwork bool
	Year       int
}

// DetailService is the narrow seam the harness drives — the real
// GetArtistContentService adapted by the cmd. Kept here (not the concrete type)
// so this package never imports service and stays a pure, testable core.
type DetailService interface {
	Albums(ctx context.Context, seedProvider, seedID, artistName string) []DetailItem
	TopTracks(ctx context.Context, seedProvider, seedID, artistName string) []DetailItem
}

// DetailReport is the gated result. Metric names follow the "<mode>.<metric>"
// convention; contamination is lower-is-better (0 is clean), the rest higher.
type DetailReport struct {
	Goldens            int             `json:"goldens"`
	ContaminationCount int             `json:"contamination_count"`
	AlbumRecall        float64         `json:"album_recall"`
	TrackRecall        float64         `json:"track_recall"`
	MetadataCoverage   float64         `json:"metadata_coverage"`
	PerArtist          []DetailArtist  `json:"per_artist"`
	Fails              []FailureRecord `json:"failures"`
}

// DetailArtist is one golden's scored outcome, for the human render + JSON.
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

// RunDetailEval scores every golden through svc and aggregates the report.
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
		// Average recall only over goldens that declare expected titles, so a
		// metadata-only control (empty expected) doesn't inflate the rate to 1.
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

// countContamination counts items carrying a forbidden source or a forbidden
// title (the other same-name artist's fingerprints) and logs each as a failure.
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

// recall is the fraction of expected titles present in the returned items
// (normalized substring match — an expected "rest in bass" counts as present
// whether it comes back exactly or as "rest in bass: encore").
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

// metadataCoverage is the fraction of albums that came back with BOTH artwork
// and a year (a complete card). Returns hasAlbums=false when there are none, so
// an empty artist doesn't drag the average toward zero.
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
