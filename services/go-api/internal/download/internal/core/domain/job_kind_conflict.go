package domain

import "errors"

var ErrJobKindConflict = errors.New("acquisition job of a different kind is already in flight")
