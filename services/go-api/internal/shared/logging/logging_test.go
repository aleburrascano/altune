package logging_test

import (
	"altune/go-api/internal/shared/logging"
	"log/slog"
	"os"
	"testing"
)

func setupLogger(t *testing.T, level string, development bool) (*slog.Logger, *logging.RingBuffer, func() string) {
	t.Helper()
	stdout, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatalf("create stdout file: %v", err)
	}
	prevDefault := slog.Default()
	t.Cleanup(func() {
		slog.SetDefault(prevDefault)
		_ = stdout.Close()
	})
	ring := setupWritingTo(stdout, level, development)
	return slog.Default(), ring, func() string { return readFile(t, stdout.Name()) }
}

func setupWritingTo(stdout *os.File, level string, development bool) *logging.RingBuffer {
	realStdout := os.Stdout
	os.Stdout = stdout
	defer func() { os.Stdout = realStdout }()
	if development {
		return logging.SetupDevelopment(level)
	}
	return logging.Setup(level)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
