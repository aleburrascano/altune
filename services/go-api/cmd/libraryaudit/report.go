package main

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

type summary struct {
	ReadyTracks             int `json:"ready_tracks"`
	Checked                 int `json:"checked"`
	NoISRC                  int `json:"no_isrc"`
	UnknownISRC             int `json:"unknown_isrc"`
	ISRCConfirmed           int `json:"isrc_confirmed,omitempty"`
	LookupFailed            int `json:"lookup_failed"`
	WrongSong               int `json:"wrong_song"`
	LengthShorter           int `json:"length_shorter"`
	LengthLonger            int `json:"length_longer"`
	YearFillable            int `json:"year_fillable"`
	AlbumArtistFillable     int `json:"album_artist_fillable"`
	ISRCFillable            int `json:"isrc_fillable"`
	ISRCAmbiguous           int `json:"isrc_ambiguous"`
	YearOtherRelease        int `json:"year_other_release"`
	AlbumArtistOtherRelease int `json:"album_artist_other_release"`
	CoverURLDiffers         int `json:"cover_url_differs"`
	CoverImageDiffers       int `json:"cover_image_differs"`
	CoverUnreadable         int `json:"cover_unreadable"`

	TrackNumberFillable        int `json:"track_number_fillable,omitempty"`
	TrackNumberUnfillable      int `json:"track_number_unfillable,omitempty"`
	GenreFillable              int `json:"genre_fillable,omitempty"`
	GenreUnfillable            int `json:"genre_unfillable,omitempty"`
	CatalogDurationFillable    int `json:"catalog_duration_fillable,omitempty"`
	CatalogDurationCorrectable int `json:"catalog_duration_correctable,omitempty"`
	YearFromRelease            int `json:"year_from_release,omitempty"`
	AlbumArtistFromRelease     int `json:"album_artist_from_release,omitempty"`
	CoverBlank                 int `json:"cover_blank,omitempty"`
	CoverNotReplaceable        int `json:"cover_not_replaceable,omitempty"`
	YearUnfillable             int `json:"year_unfillable,omitempty"`
	CatalogDurationUnfillable  int `json:"catalog_duration_unfillable,omitempty"`
}

type report struct {
	GeneratedAt time.Time `json:"generated_at"`
	Aborted     bool      `json:"aborted"`
	AbortReason string    `json:"abort_reason,omitempty"`
	Summary     summary   `json:"summary"`
	Findings    []finding `json:"findings"`
}

func countCategories(findings []finding) map[category]int {
	counts := map[category]int{}
	for _, f := range findings {
		counts[f.Category]++
	}
	return counts
}

func summarize(outcome auditOutcome) summary {
	counts := countCategories(outcome.Findings)
	return summary{
		ReadyTracks:                outcome.Total,
		Checked:                    outcome.Checked,
		NoISRC:                     counts[categoryNoISRC],
		UnknownISRC:                counts[categoryUnknownISRC],
		ISRCConfirmed:              counts[categoryISRCConfirmed],
		LookupFailed:               counts[categoryLookupFailed],
		WrongSong:                  counts[categoryWrongSong],
		LengthShorter:              counts[categoryLengthShorter],
		LengthLonger:               counts[categoryLengthLonger],
		YearFillable:               counts[categoryYearFillable],
		AlbumArtistFillable:        counts[categoryAlbumArtistFillable],
		ISRCFillable:               counts[categoryISRCFillable],
		ISRCAmbiguous:              counts[categoryISRCAmbiguous],
		YearOtherRelease:           counts[categoryYearOtherRelease],
		AlbumArtistOtherRelease:    counts[categoryAlbumArtistOtherRelease],
		CoverURLDiffers:            counts[categoryCoverURLDiffers],
		CoverImageDiffers:          counts[categoryCoverImageDiffers],
		CoverUnreadable:            counts[categoryCoverUnreadable],
		TrackNumberFillable:        counts[categoryTrackNumberFillable],
		TrackNumberUnfillable:      counts[categoryTrackNumberUnfillable],
		GenreFillable:              counts[categoryGenreFillable],
		GenreUnfillable:            counts[categoryGenreUnfillable],
		CatalogDurationFillable:    counts[categoryCatalogDurationFillable],
		CatalogDurationCorrectable: counts[categoryCatalogDurationCorrectable],
		YearFromRelease:            counts[categoryYearFromRelease],
		AlbumArtistFromRelease:     counts[categoryAlbumArtistFromRelease],
		CoverBlank:                 counts[categoryCoverBlank],
		CoverNotReplaceable:        counts[categoryCoverNotReplaceable],
		YearUnfillable:             counts[categoryYearUnfillable],
		CatalogDurationUnfillable:  counts[categoryCatalogDurationUnfillable],
	}
}

func buildReport(outcome auditOutcome, now time.Time) report {
	findings := outcome.Findings
	if findings == nil {
		findings = []finding{}
	}
	return report{
		GeneratedAt: now.UTC(),
		Aborted:     outcome.AbortReason != "",
		AbortReason: outcome.AbortReason,
		Summary:     summarize(outcome),
		Findings:    findings,
	}
}

func writeJSON(w io.Writer, r report) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(r); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}

func writeSummary(w io.Writer, r report) {
	s := r.Summary
	if r.Aborted {
		_, _ = fmt.Fprintf(w, "ABORTED, partial report: %s\n", r.AbortReason)
	}
	_, _ = fmt.Fprintf(w, "%d ready tracks\n", s.ReadyTracks)
	_, _ = fmt.Fprintf(w, "  checked by ISRC:            %d\n", s.Checked)
	_, _ = fmt.Fprintf(w, "  no ISRC:                    %d (deezer found one ISRC for %d, ambiguous for %d)\n", s.NoISRC, s.ISRCFillable, s.ISRCAmbiguous)
	_, _ = fmt.Fprintf(w, "  unknown ISRC:               %d (musicbrainz confirms %d)\n", s.UnknownISRC+s.ISRCConfirmed, s.ISRCConfirmed)
	_, _ = fmt.Fprintf(w, "  lookup failed:              %d\n", s.LookupFailed)
	_, _ = fmt.Fprintf(w, "  ISRC is a different song:   %d\n", s.WrongSong)
	_, _ = fmt.Fprintf(w, "  audio >10s shorter:         %d\n", s.LengthShorter)
	_, _ = fmt.Fprintf(w, "  audio >10s longer:          %d\n", s.LengthLonger)
	triage := countCategories(r.Findings)
	_, _ = fmt.Fprintf(w, "    ISRC is another version:  %d, to re-acquire: %d\n", triage[categoryLengthVersionFix], triage[categoryLengthReacquire])
	_, _ = fmt.Fprintf(w, "  year fillable:              %d\n", s.YearFillable)
	_, _ = fmt.Fprintf(w, "  album artist fillable:      %d\n", s.AlbumArtistFillable)
	_, _ = fmt.Fprintf(w, "  year from other release:    %d\n", s.YearOtherRelease)
	_, _ = fmt.Fprintf(w, "  album artist other release: %d\n", s.AlbumArtistOtherRelease)
	_, _ = fmt.Fprintf(w, "  year via musicbrainz:       %d\n", s.YearFromRelease)
	_, _ = fmt.Fprintf(w, "  album artist via musicbrainz: %d\n", s.AlbumArtistFromRelease)
	_, _ = fmt.Fprintf(w, "  track number fillable:      %d (unfillable: %d)\n", s.TrackNumberFillable, s.TrackNumberUnfillable)
	_, _ = fmt.Fprintf(w, "  genre fillable:             %d (unfillable: %d)\n", s.GenreFillable, s.GenreUnfillable)
	_, _ = fmt.Fprintf(w, "  year unfillable:            %d\n", s.YearUnfillable)
	_, _ = fmt.Fprintf(w, "  catalog length fillable:    %d (unfillable: %d, corrected: %d)\n", s.CatalogDurationFillable, s.CatalogDurationUnfillable, s.CatalogDurationCorrectable)
	_, _ = fmt.Fprintf(w, "  cover URL differs:          %d\n", s.CoverURLDiffers)
	_, _ = fmt.Fprintf(w, "  cover image differs:        %d (unreadable: %d, not replaceable: %d)\n", s.CoverImageDiffers, s.CoverUnreadable, s.CoverNotReplaceable)
	_, _ = fmt.Fprintf(w, "  blank cover image:          %d\n", s.CoverBlank)
}
