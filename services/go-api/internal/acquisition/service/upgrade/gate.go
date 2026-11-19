package upgrade

import (
	acqdomain "altune/go-api/internal/acquisition/internal/core/domain"
	"altune/go-api/internal/acquisition/internal/core/grade"
	"altune/go-api/internal/acquisition/internal/core/ports"
	"errors"
	"fmt"
)

var ErrNotAnUpgrade = errors.New("the new file does not grade strictly above the current one")

type Gate struct {
	Current grade.Grade

	Stored    ports.SourceFormat
	FloorKbps int

	CatalogSeconds float64
	StoredSeconds  float64
}

func (g Gate) Admit(candidate grade.Grade) error {
	if g.Current == "" {
		return nil
	}
	if !candidate.Beats(g.Current) {
		return fmt.Errorf("%w: %s against %s", ErrNotAnUpgrade, candidate, g.Current)
	}
	return nil
}

func (g Gate) AdmitFetched(candidate grade.Grade, fetched ports.SourceFormat, fetchedSeconds float64) error {
	if g.betterStreamOfSameGrade(candidate, fetched) || g.fitsCatalogLengthOfSameGrade(candidate, fetchedSeconds) {
		return nil
	}
	return g.Admit(candidate)
}

func (g Gate) RefetchesCurrentSource() bool {
	return g.Current.IsKnown() && g.Stored.IsLowBitrate(g.FloorKbps)
}

func (g Gate) betterStreamOfSameGrade(candidate grade.Grade, fetched ports.SourceFormat) bool {
	return g.RefetchesCurrentSource() &&
		candidate == g.Current &&
		!fetched.IsLowBitrate(g.FloorKbps) &&
		fetched.Kbps > g.Stored.Kbps
}

func (g Gate) fitsCatalogLengthOfSameGrade(candidate grade.Grade, fetchedSeconds float64) bool {
	return g.Current.IsKnown() &&
		candidate == g.Current &&
		g.CatalogSeconds > 0 &&
		g.StoredSeconds > 0 &&
		fetchedSeconds > 0 &&
		!acqdomain.LengthAuthoritative.Agrees(g.CatalogSeconds, g.StoredSeconds) &&
		acqdomain.LengthAuthoritative.Agrees(g.CatalogSeconds, fetchedSeconds)
}
