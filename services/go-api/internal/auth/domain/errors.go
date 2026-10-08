package domain

import "errors"

var ErrTokenExpired = errors.New("auth: token expired")
