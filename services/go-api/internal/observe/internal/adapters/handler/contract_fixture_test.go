package handler_test

import (
	"altune/go-api/internal/shared/contracttest"
	"path/filepath"
	"testing"
)

func assertMatchesFixture(t *testing.T, name string, body []byte) {
	t.Helper()
	contracttest.AssertMatchesFixture(t, filepath.Join("testdata", "contract", name), body)
}
