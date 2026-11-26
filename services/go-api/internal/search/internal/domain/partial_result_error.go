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

type PartialKindsError struct {
	Failed []ResultKind
	Err    error
}

func (e *PartialKindsError) Error() string {
	return fmt.Sprintf("partial result: kinds %v failed: %v", e.Failed, e.Err)
}

func (e *PartialKindsError) Unwrap() error { return e.Err }
