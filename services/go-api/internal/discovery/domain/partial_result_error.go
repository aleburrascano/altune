package domain

import "fmt"

type PartialResultError struct {
	Page int
	Err  error
}

func (e *PartialResultError) Error() string {
	return fmt.Sprintf("partial result: page %d failed: %v", e.Page, e.Err)
}

func (e *PartialResultError) Unwrap() error { return e.Err }
