package commands

type RunMode int

const (
	DryRun RunMode = iota
	Apply
)
