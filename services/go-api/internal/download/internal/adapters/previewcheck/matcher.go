package previewcheck

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"context"
	"fmt"
)

const DefaultMatchMaxBitErrorRate = 0.18

type Matcher struct {
	previews *Previews
	printer  RawFingerprint
	matchMax float64
}

var _ ports.PreviewMatcher = (*Matcher)(nil)

func NewMatcher(previews *Previews, printer RawFingerprint, matchMax float64) *Matcher {
	return &Matcher{previews: previews, printer: printer, matchMax: matchMax}
}

func (m *Matcher) MatchesPreview(ctx context.Context, filePath string, target domain.PreviewTarget) (domain.PreviewComparison, error) {
	clip, err := m.previews.Clip(ctx, Target{ISRC: target.ISRC, DeezerTrackID: target.DeezerTrackID})
	if err != nil {
		return domain.PreviewComparison{}, err
	}
	if clip.Unavailable != "" {
		return domain.PreviewComparison{Unavailable: clip.Unavailable}, nil
	}
	filePrint, err := m.printer.Print(ctx, filePath)
	if err != nil {
		return domain.PreviewComparison{}, fmt.Errorf("fingerprint the file: %w", err)
	}
	if reason := NotComparable(filePrint, clip.Print); reason != "" {
		return domain.PreviewComparison{Unavailable: reason}, nil
	}
	aligned, err := BestOffset(filePrint, clip.Print)
	if err != nil {
		return domain.PreviewComparison{}, err
	}
	return domain.PreviewComparison{
		Matched:        aligned.BitErrorRate <= m.matchMax,
		BitErrorRate:   aligned.BitErrorRate,
		OverlapSeconds: OverlapSeconds(len(clip.Print)),
	}, nil
}
