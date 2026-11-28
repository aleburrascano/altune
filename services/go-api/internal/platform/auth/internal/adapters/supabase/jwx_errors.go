package supabase

import (
	"altune/go-api/internal/platform/auth/internal/domain"
	"errors"
	"strings"

	"github.com/lestrrat-go/jwx/v2/jwt"
)

func classifyJWTError(err error) domain.TokenRejectReason {
	switch {
	case errors.Is(err, jwt.ErrTokenExpired()):
		return domain.ReasonExpired
	case errors.Is(err, jwt.ErrInvalidIssuer()):
		return domain.ReasonClaimInvalidISS
	case errors.Is(err, jwt.ErrInvalidAudience()):
		return domain.ReasonClaimInvalidAUD
	case hasUntypedSignatureFailure(err):
		return domain.ReasonSignatureInvalid
	default:
		return domain.ReasonMalformed
	}
}

func hasUntypedSignatureFailure(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "failed to find key") ||
		strings.Contains(msg, "could not verify message")
}
