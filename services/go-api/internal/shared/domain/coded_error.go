package domain

import "time"

type Kind int

const (
	KindInternal Kind = iota
	KindInvalid
	KindUnauthenticated
	KindForbidden
	KindNotFound
	KindConflict
	KindTooLarge
	KindRateLimited
	KindUpstreamFailed
	KindUnavailable
	KindTimeout
)

type Code struct {
	name  string
	kind  Kind
	title string
}

func (c Code) String() string { return c.name }
func (c Code) Kind() Kind     { return c.kind }
func (c Code) Title() string  { return c.title }

type CodedError struct {
	code       Code
	detail     string
	retryAfter time.Duration
}

func NewError(code Code, detail string) *CodedError {
	if detail == "" {
		detail = code.title
	}
	return &CodedError{code: code, detail: detail}
}

func (e *CodedError) WithRetryAfter(wait time.Duration) *CodedError {
	retryable := *e
	retryable.retryAfter = wait
	return &retryable
}

func (e *CodedError) Error() string             { return e.detail }
func (e *CodedError) Code() Code                { return e.code }
func (e *CodedError) RetryAfter() time.Duration { return e.retryAfter }
