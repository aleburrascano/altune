package redact

import (
	"strings"
	"testing"
)

func TestSecretsInBodyMasksCredentialAfterStrayClosingByte(t *testing.T) {
	for _, body := range []string{
		`{"a":1}] "password":"hunter2"`,
		`{"a":1}} "password":"hunter2"`,
	} {
		if got := SecretsInBody(body); strings.Contains(got, "hunter2") {
			t.Errorf("SecretsInBody(%q) = %q, leaks credential", body, got)
		}
	}
}
