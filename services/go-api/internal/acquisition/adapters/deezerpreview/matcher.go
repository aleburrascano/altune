package deezerpreview

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
	"fmt"
)

const DefaultMatchMaxBitErrorRate = 0.18

type Matcher struct {
	previews *Previews
	printer  RawPrinter
	matchMax float64
}

var _ ports.PreviewMatcher = (*Matcher)(nil)

func NewMatcher(previews *Previews, printer RawPrinter, matchMax float64) *Matcher {
	return &Matcher{previews: previews, printer: printer, matchMax: matchMax}
}

func (m *Matcher) MatchesPreview(ctx context.Context, filePath string, target ports.PreviewTarget) (ports.PreviewComparison, error) {
	clip, err := m.previews.Clip(ctx, Target{ISRC: target.ISRC, DeezerTrackID: target.DeezerTrackID})
	if err != nil {
		return ports.PreviewComparison{}, err
	}
	if clip.Unavailable != "" {
		return ports.PreviewComparison{Unavailable: clip.Unavailable}, nil
	}
	filePrint, err := m.printer.Print(ctx, filePath)
	if err != nil {
		return ports.PreviewComparison{}, fmt.Errorf("fingerprint the file: %w", err)
	}
	if reason := Incomparable(filePrint, clip.Print); reason != "" {
		return ports.PreviewComparison{Unavailable: reason}, nil
	}
	aligned, err := BestAlignment(filePrint, clip.Print)
	if err != nil {
		return ports.PreviewComparison{}, err
	}
	return ports.PreviewComparison{Matched: aligned.BitErrorRate <= m.matchMax, BitErrorRate: aligned.BitErrorRate}, nil
}
