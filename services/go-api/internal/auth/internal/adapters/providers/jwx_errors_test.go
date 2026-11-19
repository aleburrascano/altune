package providers_test

import (
	"altune/go-api/internal/auth"
	"testing"
)

func TestClassifyJWTError_RealJWXSignatureFailures(t *testing.T) {
	cases := []struct {
		name string
		kid  string
	}{
		{"kid absent from key set", "rotated-kid"},
		{"wrong key under known kid", "test-key-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newTestJWTFixture(t)
			verifier := f.newVerifier(t)
			f.privateKey, f.keyID = generateRSAKey(t), tc.kid

			_, err := verifier.Verify(t.Context(), f.validToken(t))

			assertInvalidTokenReason(t, err, string(auth.ReasonSignatureInvalid))
		})
	}
}
