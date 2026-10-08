package providers

import (
	authdomain "altune/go-api/internal/auth/domain"
	"errors"
	"strings"

	"github.com/lestrrat-go/jwx/v2/jwt"
)

func classifyJWTError(err error) authdomain.TokenRejectReason {
	switch {
	case errors.Is(err, jwt.ErrTokenExpired()):
		return authdomain.ReasonExpired
	case errors.Is(err, jwt.ErrInvalidIssuer()):
		return authdomain.ReasonClaimInvalidISS
	case errors.Is(err, jwt.ErrInvalidAudience()):
		return authdomain.ReasonClaimInvalidAUD
	case hasUntypedSignatureFailure(err):
		return authdomain.ReasonSignatureInvalid
	default:
		return authdomain.ReasonMalformed
	}
}

func hasUntypedSignatureFailure(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "failed to find key") ||
		strings.Contains(msg, "could not verify message")
}
