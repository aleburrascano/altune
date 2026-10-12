package ports

import "time"

const TempDirPrefix = "altune-acquire-"

type TempWorkspace interface {
	Create(pattern string) (string, error)
	Remove(path string) error
	Size(path string) (int64, error)
	SweepAbandoned(abandonedBefore time.Time) int
}
