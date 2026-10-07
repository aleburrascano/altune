package ports

import "time"

const TempDirPrefix = "altune-acquire-"

type TempWorkspace interface {
	Create(pattern string) (string, error)
	Remove(path string) error
	SweepAbandoned(abandonedBefore time.Time) int
}
