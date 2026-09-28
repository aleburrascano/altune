package ports

import "errors"

var ErrDBTransient = errors.New("catalog database temporarily unavailable")
