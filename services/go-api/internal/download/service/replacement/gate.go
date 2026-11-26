package replacement

import (
	"altune/go-api/internal/download/internal/core/audioquality"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/download/internal/core/tracklength"
	"errors"
	"fmt"
)

var ErrNotAnUpgrade = errors.New("the new file does not grade strictly above the current one")

type ReplacementGate struct {
	Current audioquality.Grade

	Stored    ports.SourceFormat
	FloorKbps int

	CatalogSeconds float64
	StoredSeconds  float64
}

func (g ReplacementGate) Admit(candidate audioquality.Grade) error {
	if g.Current == "" {
		return nil
	}
	if !candidate.Beats(g.Current) {
		return fmt.Errorf("%w: %s against %s", ErrNotAnUpgrade, candidate, g.Current)
	}
	return nil
}

func (g ReplacementGate) AdmitFetched(candidate audioquality.Grade, fetched ports.SourceFormat, fetchedSeconds float64) error {
	if g.betterStreamOfSameGrade(candidate, fetched) || g.fitsCatalogLengthOfSameGrade(candidate, fetchedSeconds) {
		return nil
	}
	return g.Admit(candidate)
}

func (g ReplacementGate) RefetchesCurrentSource() bool {
	return g.Current.IsKnown() && g.Stored.IsLowBitrate(g.FloorKbps)
}

func (g ReplacementGate) betterStreamOfSameGrade(candidate audioquality.Grade, fetched ports.SourceFormat) bool {
	return g.RefetchesCurrentSource() &&
		candidate == g.Current &&
		!fetched.IsLowBitrate(g.FloorKbps) &&
		fetched.Kbps > g.Stored.Kbps
}

func (g ReplacementGate) fitsCatalogLengthOfSameGrade(candidate audioquality.Grade, fetchedSeconds float64) bool {
	return g.Current.IsKnown() &&
		candidate == g.Current &&
		g.CatalogSeconds > 0 &&
		g.StoredSeconds > 0 &&
		fetchedSeconds > 0 &&
		!tracklength.SameRecording(g.CatalogSeconds, g.StoredSeconds) &&
		tracklength.SameRecording(g.CatalogSeconds, fetchedSeconds)
}
