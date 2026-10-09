package domain

import (
	"errors"
	"fmt"
)

type SourceUnavailableError struct {
	Source SourceName
	Err    error
}

func (e *SourceUnavailableError) Error() string {
	return fmt.Sprintf("source %s unavailable: %v", e.Source, e.Err)
}

func (e *SourceUnavailableError) Unwrap() error { return e.Err }

func IsSourceUnavailable(err error) bool {
	var unavailable *SourceUnavailableError
	return errors.As(err, &unavailable)
}
