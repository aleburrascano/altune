package service

import (
	"altune/go-api/internal/acquisition/ports"
	"os"
	"sync"
	"time"
)

// osWorkspace is the os-backed ports.TempWorkspace the service tests use, since
// service tests may not import the tempfs adapter.
type osWorkspace struct{}

func (osWorkspace) Create(pattern string) (string, error) {
	return os.MkdirTemp("", ports.TempDirPrefix+pattern)
}

func (osWorkspace) Remove(path string) error { return os.RemoveAll(path) }

func (osWorkspace) SweepAbandoned(time.Time) int { return 0 }

func init() { defaultTempWorkspace = osWorkspace{} }

// callRecordingWorkspace wraps a workspace and logs every Create and Remove.
type callRecordingWorkspace struct {
	inner ports.TempWorkspace

	mu       sync.Mutex
	patterns []string
	created  []string
	removed  []string
}

func newCallRecordingWorkspace() *callRecordingWorkspace {
	return &callRecordingWorkspace{inner: osWorkspace{}}
}

func (r *callRecordingWorkspace) Create(pattern string) (string, error) {
	dir, err := r.inner.Create(pattern)
	if err == nil {
		r.mu.Lock()
		r.patterns = append(r.patterns, pattern)
		r.created = append(r.created, dir)
		r.mu.Unlock()
	}
	return dir, err
}

func (r *callRecordingWorkspace) Remove(path string) error {
	r.mu.Lock()
	r.removed = append(r.removed, path)
	r.mu.Unlock()
	return r.inner.Remove(path)
}

func (r *callRecordingWorkspace) SweepAbandoned(t time.Time) int { return r.inner.SweepAbandoned(t) }

func (r *callRecordingWorkspace) createdDirs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.created...)
}

func (r *callRecordingWorkspace) removedPaths() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.removed...)
}

func (r *callRecordingWorkspace) removedCount(path string) int {
	n := 0
	for _, p := range r.removedPaths() {
		if p == path {
			n++
		}
	}
	return n
}

// liveDirs returns created dirs that were never passed to Remove.
func (r *callRecordingWorkspace) liveDirs() []string {
	var live []string
	for _, dir := range r.createdDirs() {
		if r.removedCount(dir) == 0 {
			live = append(live, dir)
		}
	}
	return live
}
