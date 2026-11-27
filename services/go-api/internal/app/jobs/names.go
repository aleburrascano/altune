package jobs

import (
	"altune/go-api/internal/shared/logging"
	"context"
	"fmt"
	"log/slog"
)

type Name string

const (
	EvalMeter                Name = "eval meter"
	AlertMonitor             Name = "alert monitor"
	OrphanedAudioReconcile   Name = "orphaned audio reconcile"
	DiscographyEventPrune    Name = "discography event prune"
	SpellingHintsRefresh     Name = "vocabulary refresh"
	BehavioralRankingRefresh Name = "behavioral ranking refresh"
	DeletedUserDataErasure   Name = "deleted identity erasure"
	AcquisitionSourceCanary  Name = "acquisition source canary"
	StreamRecovery           Name = "stream recovery"
	AcquisitionPrune         Name = "acquisition prune"
	AcquisitionUpgrade       Name = "acquisition upgrade"
	UnreleasedRefresh        Name = "unreleased refresh"
)

type spec struct {
	name          Name
	alertable     bool
	switchable    bool
	everyInstance bool
}

var table = []spec{
	{name: OrphanedAudioReconcile, alertable: true, switchable: true},
	{name: DiscographyEventPrune, alertable: true, switchable: true},
	{name: SpellingHintsRefresh, alertable: true, switchable: true},
	{name: BehavioralRankingRefresh, alertable: true, switchable: true, everyInstance: true},
	{name: DeletedUserDataErasure, alertable: true, switchable: false},
	{name: AcquisitionSourceCanary, alertable: true, switchable: true},
	{name: StreamRecovery, alertable: false, switchable: true},
	{name: AcquisitionPrune, alertable: true, switchable: true},
	{name: AcquisitionUpgrade, alertable: true, switchable: true},
	{name: UnreleasedRefresh, alertable: true, switchable: true},
}

func Alertable() []Name { return namesWhere(func(s spec) bool { return s.alertable }) }

func namesWhere(keep func(spec) bool) []Name {
	names := make([]Name, 0, len(table))
	for _, s := range table {
		if keep(s) {
			names = append(names, s.name)
		}
	}
	return names
}

func runsOnEveryInstance(name Name) bool {
	for _, s := range table {
		if s.name == name {
			return s.everyInstance
		}
	}
	return false
}

func isSwitchable(name Name) bool {
	for _, s := range table {
		if s.name == name {
			return s.switchable
		}
	}
	return false
}

func (r *Runner) DisableAtStartup(ctx context.Context, names []string) error {
	for _, raw := range names {
		name := Name(raw)
		if !isSwitchable(name) {
			return fmt.Errorf("DISABLED_JOBS: unknown job %q", raw)
		}
		if r.Control(name).DisableIfEnabled() {
			slog.InfoContext(ctx, "job disabled at startup", logging.KeyJob, raw)
		}
	}
	return nil
}
