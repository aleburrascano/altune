package providers

import (
	"altune/go-api/internal/auth"
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
)

func TestClassifyJWTError_RealJWXSignatureFailures(t *testing.T) {
	knownKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate known key: %v", err)
	}
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate other key: %v", err)
	}

	const knownKID = "known-kid"
	publicJWK, err := jwk.FromRaw(&knownKey.PublicKey)
	if err != nil {
		t.Fatalf("build public jwk: %v", err)
	}
	_ = publicJWK.Set(jwk.KeyIDKey, knownKID)
	_ = publicJWK.Set(jwk.AlgorithmKey, jwa.RS256)
	keySet := jwk.NewSet()
	if err := keySet.AddKey(publicJWK); err != nil {
		t.Fatalf("add key: %v", err)
	}

	cases := []struct {
		name       string
		signingKey *rsa.PrivateKey
		kid        string
	}{
		{"kid absent from key set", otherKey, "rotated-kid"},
		{"wrong key under known kid", otherKey, knownKID},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			signer := signingJWK(t, tc.signingKey, tc.kid)
			builder := jwt.New()
			_ = builder.Set("exp", time.Now().Add(time.Hour))
			signed, err := jwt.Sign(builder, jwt.WithKey(jwa.RS256, signer))
			if err != nil {
				t.Fatalf("sign: %v", err)
			}

			_, parseErr := jwt.Parse(signed, jwt.WithKeySet(keySet), jwt.WithValidate(true))
			if parseErr == nil {
				t.Fatal("expected jwt.Parse to fail")
			}

			if got := classifyJWTError(parseErr); got != auth.ReasonSignatureInvalid {
				t.Fatalf("classifyJWTError(%q) = %v, want %v", parseErr, got, auth.ReasonSignatureInvalid)
			}
		})
	}
}
