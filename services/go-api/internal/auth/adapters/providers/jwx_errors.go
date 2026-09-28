package providers

import (
	"altune/go-api/internal/auth"
	"errors"
	"strings"

	"github.com/lestrrat-go/jwx/v2/jwt"
)

func classifyJWTError(err error) auth.TokenRejectReason {
	switch {
	case errors.Is(err, jwt.ErrTokenExpired()):
		return auth.ReasonExpired
	case errors.Is(err, jwt.ErrInvalidIssuer()):
		return auth.ReasonClaimInvalidISS
	case errors.Is(err, jwt.ErrInvalidAudience()):
		return auth.ReasonClaimInvalidAUD
	case hasUntypedSignatureFailure(err):
		return auth.ReasonSignatureInvalid
	default:
		return auth.ReasonMalformed
	}
}

func hasUntypedSignatureFailure(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "failed to find key") ||
		strings.Contains(msg, "could not verify message")
}
