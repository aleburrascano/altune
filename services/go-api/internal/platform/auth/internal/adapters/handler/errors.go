package handler

import (
	"altune/go-api/internal/platform/auth/internal/domain"
	"altune/go-api/internal/shared/apierror"
)

var rejectionCodes = map[domain.TokenRejectReason]apierror.Code{
	domain.ReasonMissing:          apierror.CodeAuthTokenMissing,
	domain.ReasonMalformed:        apierror.CodeAuthTokenMalformed,
	domain.ReasonSignatureInvalid: apierror.CodeAuthTokenSignatureInvalid,
	domain.ReasonExpired:          apierror.CodeAuthTokenExpired,
	domain.ReasonRevoked:          apierror.CodeAuthTokenRevoked,
	domain.ReasonClaimMissingEXP:  apierror.CodeAuthTokenClaimMissingEXP,
	domain.ReasonClaimInvalidISS:  apierror.CodeAuthTokenClaimInvalidISS,
	domain.ReasonClaimInvalidAUD:  apierror.CodeAuthTokenClaimInvalidAUD,
	domain.ReasonClaimInvalidSUB:  apierror.CodeAuthTokenClaimInvalidSUB,
	domain.ReasonClaimInvalidIAT:  apierror.CodeAuthTokenClaimInvalidIAT,
}

func rejectionCode(reason domain.TokenRejectReason) apierror.Code {
	if code, ok := rejectionCodes[reason]; ok {
		return code
	}
	return apierror.CodeAuthTokenInvalid
}
