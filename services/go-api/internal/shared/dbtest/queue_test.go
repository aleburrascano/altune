package dbtest_test

import (
	"altune/go-api/internal/shared/dbtest"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"
)

type cleanupRecorder struct {
	testing.TB
	cleanups []func()
	failure  string
}

func (r *cleanupRecorder) Cleanup(fn func()) { r.cleanups = append(r.cleanups, fn) }

func (r *cleanupRecorder) Fatalf(format string, args ...any) {
	r.failure = fmt.Sprintf(format, args...)
	runtime.Goexit()
}

func (r *cleanupRecorder) finish() {
	for i := len(r.cleanups) - 1; i >= 0; i-- {
		r.cleanups[i]()
	}
}

func TestExclusiveAcquisitionQueue_AnotherTestWaitsUntilTheHolderEnds(t *testing.T) {
	pool := dbtest.Pool(t)
	holder := &cleanupRecorder{TB: t}
	dbtest.ExclusiveAcquisitionQueue(holder, pool)
	releaseHolder := sync.OnceFunc(holder.finish)

	waiter := &cleanupRecorder{TB: t}
	acquired := make(chan struct{})
	go func() {
		defer close(acquired)
		dbtest.ExclusiveAcquisitionQueue(waiter, pool)
	}()
	t.Cleanup(func() {
		releaseHolder()
		select {
		case <-acquired:
			waiter.finish()
		case <-time.After(10 * time.Second):
		}
	})

	select {
	case <-acquired:
		t.Fatal("a second test got the acquisition queue while the first still held it")
	case <-time.After(300 * time.Millisecond):
	}

	releaseHolder()
	select {
	case <-acquired:
	case <-time.After(10 * time.Second):
		t.Fatal("the second test never got the acquisition queue after the first ended")
	}
	if waiter.failure != "" {
		t.Fatalf("the second test failed to take the acquisition queue: %s", waiter.failure)
	}
}
