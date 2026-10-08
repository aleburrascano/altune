package domain

import "errors"

var (
	ErrUserConnLimit   = errors.New("sse: per-user connection limit reached")
	ErrGlobalConnLimit = errors.New("sse: global connection limit reached")
)
