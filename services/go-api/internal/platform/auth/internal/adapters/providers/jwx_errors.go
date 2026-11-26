package providers

import (
	"altune/go-api/internal/platform/auth/internal/core/ports"
	"errors"
	"strings"

	"github.com/lestrrat-go/jwx/v2/jwt"
)

func classifyJWTError(err error) ports.TokenRejectReason {
	switch {
	case errors.Is(err, jwt.ErrTokenExpired()):
		return ports.ReasonExpired
	case errors.Is(err, jwt.ErrInvalidIssuer()):
		return ports.ReasonClaimInvalidISS
	case errors.Is(err, jwt.ErrInvalidAudience()):
		return ports.ReasonClaimInvalidAUD
	case hasUntypedSignatureFailure(err):
		return ports.ReasonSignatureInvalid
	default:
		return ports.ReasonMalformed
	}
}

func hasUntypedSignatureFailure(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "failed to find key") ||
		strings.Contains(msg, "could not verify message")
}
