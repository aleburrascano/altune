package panicguard_test

import (
	"altune/overseer/internal/panicguard"
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestTryReportsAPanicAsAnErrorCarryingItsValue(t *testing.T) {
	err := panicguard.Try(func() { panic("boom") })
	if err == nil {
		t.Fatal("Try returned nil for a panic")
	}
	value, ok := panicguard.Recovered(err)
	if !ok || value != "boom" {
		t.Fatalf("Recovered = %v, %v, want boom, true", value, ok)
	}
}

func TestTryReturnsNilWhenNothingPanics(t *testing.T) {
	if err := panicguard.Try(func() {}); err != nil {
		t.Fatalf("Try = %v, want nil", err)
	}
}

func TestRecoveredFindsAPanicInsideAWrappedError(t *testing.T) {
	wrapped := errors.Join(errors.New("context"), panicguard.Try(func() { panic(42) }))
	value, ok := panicguard.Recovered(wrapped)
	if !ok || value != 42 {
		t.Fatalf("Recovered = %v, %v, want 42, true", value, ok)
	}
	if _, ok := panicguard.Recovered(errors.New("plain")); ok {
		t.Fatal("Recovered reported a panic for a plain error")
	}
}

func TestCallReturnsTheValueOrTheZeroValueWithTheError(t *testing.T) {
	got, err := panicguard.Call(func() int { return 7 })
	if got != 7 || err != nil {
		t.Fatalf("Call = %d, %v, want 7, nil", got, err)
	}
	got, err = panicguard.Call(func() int { panic("boom") })
	if got != 0 || err == nil {
		t.Fatalf("Call of a panic = %d, %v, want 0 and an error", got, err)
	}
}

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buf
}

func TestContainLogsOnceAtErrorWithItsAttributesAndTheValue(t *testing.T) {
	logs := captureLogs(t)
	panicguard.Contain(context.Background(), "source pump panicked", func() { panic("boom") }, "bucket", "usage")

	out := logs.String()
	for _, want := range []string{"level=ERROR", "source pump panicked", "bucket=usage", "recover=boom"} {
		if !strings.Contains(out, want) {
			t.Errorf("log %q lacks %q", out, want)
		}
	}
	if n := strings.Count(out, "\n"); n != 1 {
		t.Errorf("logged %d lines, want 1", n)
	}
	if strings.Index(out, "bucket=usage") > strings.Index(out, "recover=boom") {
		t.Errorf("log %q puts recover before the attributes it was given", out)
	}
}

func TestContainDoesNotWriteIntoTheCallersAttributeSlice(t *testing.T) {
	captureLogs(t)
	attrs := make([]any, 2, 8)
	attrs[0], attrs[1] = "bucket", "usage"
	panicguard.Contain(context.Background(), "msg", func() { panic("boom") }, attrs...)

	if got := attrs[:3][2]; got != nil {
		t.Fatalf("Contain wrote %v into the spare capacity of the caller's slice", got)
	}
}

func TestContainSaysNothingWhenNothingPanics(t *testing.T) {
	logs := captureLogs(t)
	ran := false
	panicguard.Contain(context.Background(), "never", func() { ran = true })

	if !ran || logs.Len() != 0 {
		t.Fatalf("ran = %v, logs = %q, want it to run and log nothing", ran, logs.String())
	}
}

func TestGroupWaitsForEveryGoroutineAndReportsEachPanic(t *testing.T) {
	var g panicguard.Group
	var mu sync.Mutex
	done := 0
	for range 3 {
		g.Go(func() {
			mu.Lock()
			done++
			mu.Unlock()
		})
	}
	g.Go(func() { panic("first") })
	g.Go(func() { panic("second") })

	err := g.Wait()
	if done != 3 {
		t.Fatalf("%d goroutines finished before Wait returned, want 3", done)
	}
	if err == nil || !strings.Contains(err.Error(), "first") || !strings.Contains(err.Error(), "second") {
		t.Fatalf("Wait = %v, want both panics", err)
	}
}

func TestOnceRunsItsFunctionOnceHoweverOftenAndFromHoweverManyCallers(t *testing.T) {
	var once panicguard.Once
	var runs atomic.Int32
	var callers sync.WaitGroup
	for range 8 {
		callers.Go(func() { once.Go(func() { runs.Add(1) }) })
	}
	callers.Wait()
	if err := once.Wait(); err != nil {
		t.Fatalf("Wait = %v, want nil", err)
	}
	if got := runs.Load(); got != 1 {
		t.Fatalf("function ran %d times, want 1", got)
	}
}

func TestOnceContainLogsAPanicAndStillReturnsToTheCaller(t *testing.T) {
	logs := captureLogs(t)
	var once panicguard.Once
	once.Contain(context.Background(), "pump panicked", func() { panic("boom") }, "bucket", "logs")

	if err := once.Wait(); err != nil {
		t.Fatalf("Wait = %v, want nil because Contain already logged the panic", err)
	}
	if out := logs.String(); !strings.Contains(out, "pump panicked") || !strings.Contains(out, "recover=boom") {
		t.Fatalf("log %q lacks the contained panic", out)
	}
}

func TestOnceGoReportsAPanicThroughWait(t *testing.T) {
	var once panicguard.Once
	once.Go(func() { panic("boom") })
	if err := once.Wait(); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("Wait = %v, want the panic", err)
	}
}

func TestWaitWithinReportsWhetherTheWorkFinishedInTime(t *testing.T) {
	if !panicguard.WaitWithin(time.Second, func() {}) {
		t.Fatal("WaitWithin gave up on work that finished at once")
	}
	release := make(chan struct{})
	defer close(release)
	if panicguard.WaitWithin(20*time.Millisecond, func() { <-release }) {
		t.Fatal("WaitWithin reported work that is still blocked as finished")
	}
}

func TestGroupWithNoPanicWaitsToNil(t *testing.T) {
	var g panicguard.Group
	g.Go(func() {})
	if err := g.Wait(); err != nil {
		t.Fatalf("Wait = %v, want nil", err)
	}
}
