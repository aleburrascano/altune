package auth

import domain "altune/go-api/internal/auth/domain"

type (
	InvalidTokenError = domain.InvalidTokenError
	VerifiedToken     = domain.VerifiedToken
)

const ReasonSignatureInvalid = domain.ReasonSignatureInvalid
