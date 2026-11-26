package audiofiles_test

import (
	"testing"
	"time"
)

func makeStalledAudio(t *testing.T, dir, ref string) {
	t.Helper()
	t.Skip("mkfifo has no Windows equivalent")
}

func makeFifo(t *testing.T, dir, ref string) string {
	t.Helper()
	t.Skip("mkfifo has no Windows equivalent")
	return ""
}

func readerAppears(path string, within time.Duration) bool {
	return false
}

func lateReaderClosed(t *testing.T, path string) bool {
	t.Helper()
	t.Skip("mkfifo has no Windows equivalent")
	return false
}

func otherFilesystemDir(t *testing.T, than string) string {
	t.Helper()
	t.Skipf("no second filesystem to move %s across on windows", than)
	return ""
}
