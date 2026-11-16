package storage

import "testing"

func makeStalledAudio(t *testing.T, dir, ref string) {
	t.Helper()
	t.Skip("mkfifo has no Windows equivalent")
}
