package domain

import (
	"strconv"
	"time"
)

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

var kindNames = map[Kind]string{
	KindInternal:        "internal",
	KindInvalid:         "invalid",
	KindUnauthenticated: "unauthenticated",
	KindForbidden:       "forbidden",
	KindNotFound:        "not_found",
	KindConflict:        "conflict",
	KindTooLarge:        "too_large",
	KindRateLimited:     "rate_limited",
	KindUpstreamFailed:  "upstream_failed",
	KindUnavailable:     "unavailable",
	KindTimeout:         "timeout",
}

func (k Kind) String() string {
	if name, ok := kindNames[k]; ok {
		return name
	}
	return "kind(" + strconv.Itoa(int(k)) + ")"
}

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
	if code == (Code{}) {
		code = CodeInternal
	}
	if detail == "" {
		detail = code.title
	}
	return &CodedError{code: code, detail: detail}
}

func (e *CodedError) WithRetryAfter(wait time.Duration) *CodedError {
	retryable := *e
	retryable.retryAfter = max(wait, 0)
	return &retryable
}

func (e *CodedError) Is(target error) bool {
	other, ok := target.(*CodedError)
	return ok && other != nil && other.code == e.code
}

func (e *CodedError) Error() string             { return e.detail }
func (e *CodedError) Code() Code                { return e.code }
func (e *CodedError) RetryAfter() time.Duration { return e.retryAfter }
