package authn

import "altune/overseer/internal/authn/internal/adapters/verifier"

type (
	Claims   = verifier.Claims
	Verifier = verifier.Verifier
)

var (
	ErrNoKey     = verifier.ErrNoKey
	ErrNoSubject = verifier.ErrNoSubject
	ErrNoIssuer  = verifier.ErrNoIssuer
)

var New = verifier.New
