package service

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"errors"
	"fmt"
)

var ErrNotAnUpgrade = errors.New("the new file does not grade strictly above the current one")

type ReplacementGate struct {
	Current domain.Grade

	Stored    ports.SourceFormat
	FloorKbps int

	CatalogSeconds float64
	StoredSeconds  float64
}

func (g ReplacementGate) Admit(candidate domain.Grade) error {
	if g.Current == "" {
		return nil
	}
	if !candidate.Beats(g.Current) {
		return fmt.Errorf("%w: %s against %s", ErrNotAnUpgrade, candidate, g.Current)
	}
	return nil
}

func (g ReplacementGate) AdmitFetched(candidate domain.Grade, fetched ports.SourceFormat, fetchedSeconds float64) error {
	if g.betterStreamOfSameGrade(candidate, fetched) || g.fitsCatalogLengthOfSameGrade(candidate, fetchedSeconds) {
		return nil
	}
	return g.Admit(candidate)
}

func (g ReplacementGate) RefetchesCurrentSource() bool {
	return g.Current.IsKnown() && g.Stored.IsLowBitrate(g.FloorKbps)
}

func (g ReplacementGate) betterStreamOfSameGrade(candidate domain.Grade, fetched ports.SourceFormat) bool {
	return g.RefetchesCurrentSource() &&
		candidate == g.Current &&
		!fetched.IsLowBitrate(g.FloorKbps) &&
		fetched.Kbps > g.Stored.Kbps
}

func (g ReplacementGate) fitsCatalogLengthOfSameGrade(candidate domain.Grade, fetchedSeconds float64) bool {
	return g.Current.IsKnown() &&
		candidate == g.Current &&
		g.CatalogSeconds > 0 &&
		g.StoredSeconds > 0 &&
		fetchedSeconds > 0 &&
		!SameRecording(g.CatalogSeconds, g.StoredSeconds) &&
		SameRecording(g.CatalogSeconds, fetchedSeconds)
}
