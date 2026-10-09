package domain

import "time"

type UnavailableError struct{}

func (*UnavailableError) Error() string     { return "queue state temporarily unavailable" }
func (*UnavailableError) HTTPStatus() int   { return 503 }
func (*UnavailableError) ErrorCode() string { return "playback.unavailable" }

func (*UnavailableError) RetryAfter() time.Duration { return time.Second }

var ErrQueueStateUnavailable error = &UnavailableError{}
