package ports

import "time"

const TempDirPrefix = "altune-acquire-"

// TempWorkspace owns the temp directories an acquisition works in.
type TempWorkspace interface {
	// Create makes a directory under the OS temp root named TempDirPrefix+pattern.
	Create(pattern string) (string, error)
	// Remove deletes a file or tree; a missing path is not an error.
	Remove(path string) error
	// SweepAbandoned removes prefixed directories last modified before
	// abandonedBefore and returns how many it removed.
	SweepAbandoned(abandonedBefore time.Time) int
}
