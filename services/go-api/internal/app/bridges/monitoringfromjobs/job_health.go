package monitoringfromjobs

import "altune/go-api/internal/platform/monitoring"

type Control interface {
	ConsecutiveFailures() int64
	Disabled() bool
}

type jobHealth struct {
	names    []string
	controls map[string]Control
}

func NewJobHealth(names []string, control func(name string) Control) monitoring.JobHealthReader {
	controls := make(map[string]Control, len(names))
	for _, name := range names {
		controls[name] = control(name)
	}
	return jobHealth{names: names, controls: controls}
}

func (j jobHealth) Alertable() []string { return j.names }

func (j jobHealth) State(name string) monitoring.JobState {
	c := j.controls[name]
	return monitoring.JobState{ConsecutiveFailures: c.ConsecutiveFailures(), Disabled: c.Disabled()}
}
