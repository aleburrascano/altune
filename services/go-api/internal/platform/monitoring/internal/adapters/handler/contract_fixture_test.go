package handler_test

import (
	"altune/go-api/internal/shared/jsoncontract"
	"path/filepath"
	"testing"
)

func assertMatchesFixture(t *testing.T, name string, body []byte) {
	t.Helper()
	jsoncontract.AssertMatchesFixture(t, filepath.Join("testdata", "contract", name), body)
}
