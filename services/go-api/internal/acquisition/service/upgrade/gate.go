package upgrade

import (
	"altune/go-api/internal/acquisition/service/grade"
	"errors"
	"fmt"
)

var (
	ErrNotAnUpgrade = errors.New("the new file does not grade strictly above the current one")
	ErrWouldUpgrade = errors.New("dry run: the new file would replace the current one")
)

type Gate struct {
	Current grade.Grade
	DryRun  bool
}

func (g Gate) Admit(candidate grade.Grade) error {
	if g.Current == "" {
		return nil
	}
	if !candidate.Beats(g.Current) {
		return fmt.Errorf("%w: %s against %s", ErrNotAnUpgrade, candidate, g.Current)
	}
	if g.DryRun {
		return fmt.Errorf("%w: %s over %s", ErrWouldUpgrade, candidate, g.Current)
	}
	return nil
}
