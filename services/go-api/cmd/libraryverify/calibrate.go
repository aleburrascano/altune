package main

import (
	"altune/go-api/internal/shared/textnorm"
	"context"
	"fmt"
	"io"
	"math"
	"sort"
	"time"
)

const differentPairsPerTrack = 2

type printedTrack struct {
	track libraryTrack
	file  []uint32
	clip  previewClip
}

type pairScore struct {
	TrackID      string  `json:"track_id"`
	OtherTrackID string  `json:"other_track_id"`
	Artist       string  `json:"artist"`
	Title        string  `json:"title"`
	OtherTitle   string  `json:"other_title"`
	BitErrorRate float64 `json:"bit_error_rate"`
}

type distribution struct {
	Count  int     `json:"count"`
	Min    float64 `json:"min"`
	P05    float64 `json:"p05"`
	Median float64 `json:"median"`
	P95    float64 `json:"p95"`
	Max    float64 `json:"max"`
}

type calibrationReport struct {
	GeneratedAt time.Time    `json:"generated_at"`
	Skipped     []string     `json:"skipped,omitempty"`
	Same        distribution `json:"same_song"`
	Different   distribution `json:"different_song"`
	SamePairs   []pairScore  `json:"same_song_pairs"`
	DiffPairs   []pairScore  `json:"different_song_pairs"`
}

func summarize(pairs []pairScore) distribution {
	rates := make([]float64, len(pairs))
	for i, pair := range pairs {
		rates[i] = pair.BitErrorRate
	}
	sort.Float64s(rates)
	if len(rates) == 0 {
		return distribution{}
	}
	return distribution{
		Count: len(rates), Min: rates[0], P05: quantile(rates, 0.05), Median: quantile(rates, 0.5),
		P95: quantile(rates, 0.95), Max: rates[len(rates)-1],
	}
}

func quantile(sorted []float64, q float64) float64 {
	rank := int(math.Ceil(q*float64(len(sorted)))) - 1
	return sorted[max(rank, 0)]
}

func scorePairs(printed []printedTrack) (same, different []pairScore) {
	for i, own := range printed {
		if aligned, err := bestAlignment(own.file, own.clip.Print); err == nil {
			same = append(same, pairScore{
				TrackID: own.track.ID, OtherTrackID: own.track.ID, Artist: own.track.Artist,
				Title: own.track.Title, OtherTitle: own.track.Title, BitErrorRate: aligned.BitErrorRate,
			})
		}
		different = append(different, differentSongScores(printed, i)...)
	}
	return same, different
}

func differentSongScores(printed []printedTrack, index int) []pairScore {
	own := printed[index]
	var scores []pairScore
	for j, other := range printed {
		if len(scores) == differentPairsPerTrack {
			break
		}
		if j == index || !isDifferentSongBySameArtist(own.track, other.track) {
			continue
		}
		if aligned, err := bestAlignment(own.file, other.clip.Print); err == nil {
			scores = append(scores, pairScore{
				TrackID: own.track.ID, OtherTrackID: other.track.ID, Artist: own.track.Artist,
				Title: own.track.Title, OtherTitle: other.track.Title, BitErrorRate: aligned.BitErrorRate,
			})
		}
	}
	return scores
}

func isDifferentSongBySameArtist(a, b libraryTrack) bool {
	return textnorm.NormalizeForMatch(a.Artist) == textnorm.NormalizeForMatch(b.Artist) &&
		textnorm.NormalizeForMatch(a.Title) != textnorm.NormalizeForMatch(b.Title) &&
		a.Identity.ISRC != b.Identity.ISRC
}

func (v *verifier) calibrate(ctx context.Context, tracks []libraryTrack) calibrationReport {
	var printed []printedTrack
	var skipped []string
	for _, track := range tracks {
		sample, err := v.printTrack(ctx, track)
		if err != nil {
			skipped = append(skipped, fmt.Sprintf("%s %s - %s: %v", track.ID, track.Artist, track.Title, err))
			continue
		}
		printed = append(printed, sample)
		v.reportProgress(len(tracks))
	}
	same, different := scorePairs(printed)
	return calibrationReport{
		GeneratedAt: time.Now().UTC(), Skipped: skipped, Same: summarize(same), Different: summarize(different),
		SamePairs: same, DiffPairs: different,
	}
}

func (v *verifier) printTrack(ctx context.Context, track libraryTrack) (printedTrack, error) {
	clip, err := v.previews.Clip(ctx, track.Identity)
	switch {
	case err != nil:
		return printedTrack{}, fmt.Errorf("preview: %w", err)
	case clip.Unavailable != "":
		return printedTrack{}, fmt.Errorf("preview: %s", clip.Unavailable)
	}
	printed := printedTrack{track: track, clip: clip}
	err = v.withAudio(ctx, track, func(file string) error {
		var err error
		printed.file, err = v.printer.Print(ctx, file)
		return err
	})
	return printed, err
}

func writeCalibrationSummary(out io.Writer, report calibrationReport, reportPath string) {
	for _, line := range []struct {
		name string
		dist distribution
	}{{"same song", report.Same}, {"different song", report.Different}} {
		_, _ = fmt.Fprintf(out, "%-15s n=%d min=%.3f p05=%.3f median=%.3f p95=%.3f max=%.3f\n",
			line.name, line.dist.Count, line.dist.Min, line.dist.P05, line.dist.Median, line.dist.P95, line.dist.Max)
	}
	_, _ = fmt.Fprintf(out, "skipped: %d\nreport: %s\n", len(report.Skipped), reportPath)
}
