package domain

import "errors"

var ErrSearchDegraded = errors.New("search degraded: a query or source failed and nothing was found")
