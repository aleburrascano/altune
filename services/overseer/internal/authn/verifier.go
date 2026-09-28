package authn

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	jwt "github.com/golang-jwt/jwt/v5"
)

var (
	ErrNoKey          = errors.New("authn: no verification key for token")
	ErrUnsupportedAlg = errors.New("authn: unsupported token algorithm")
	ErrNoSubject      = errors.New("authn: token has no subject claim")
	ErrNoIssuer       = errors.New("authn: verifier has no expected issuer")
)

const supabaseAudience = "authenticated"

type Claims struct {
	Subject string
}

type Verifier struct {
	jwks       *jwksCache
	hsSecret   []byte
	issuer     string
	validAlgs  []string
	parserOpts []jwt.ParserOption
}

func New(jwksURL, issuer, hsSecret string, httpClient *http.Client) *Verifier {
	algs := []string{
		jwt.SigningMethodES256.Alg(),
		jwt.SigningMethodRS256.Alg(),
	}
	if hsSecret != "" {
		algs = append(algs, jwt.SigningMethodHS256.Alg())
	}
	v := &Verifier{
		jwks:      newJWKSCache(jwksURL, httpClient),
		hsSecret:  []byte(hsSecret),
		issuer:    issuer,
		validAlgs: algs,
	}
	v.parserOpts = []jwt.ParserOption{
		jwt.WithValidMethods(algs),
		jwt.WithExpirationRequired(),
		jwt.WithIssuer(issuer),
		jwt.WithAudience(supabaseAudience),
	}
	return v
}

func (v *Verifier) Verify(ctx context.Context, tokenString string) (Claims, error) {
	if v.issuer == "" {
		return Claims{}, ErrNoIssuer
	}
	var claims jwt.RegisteredClaims
	_, err := jwt.ParseWithClaims(tokenString, &claims, v.keyfunc(ctx), v.parserOpts...)
	if err != nil {
		return Claims{}, fmt.Errorf("authn: verify: %w", err)
	}
	if claims.Subject == "" {
		return Claims{}, ErrNoSubject
	}
	return Claims{Subject: claims.Subject}, nil
}

func (v *Verifier) keyfunc(ctx context.Context) jwt.Keyfunc {
	return func(t *jwt.Token) (any, error) {
		switch t.Method.(type) {
		case *jwt.SigningMethodHMAC:
			if len(v.hsSecret) == 0 {
				return nil, ErrNoKey
			}
			return v.hsSecret, nil
		case *jwt.SigningMethodRSA, *jwt.SigningMethodECDSA:
			kid, _ := t.Header["kid"].(string)
			return v.jwks.key(ctx, kid)
		default:
			return nil, ErrUnsupportedAlg
		}
	}
}
