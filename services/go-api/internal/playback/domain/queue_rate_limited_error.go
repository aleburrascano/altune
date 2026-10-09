package domain

import "net/http"

type QueueRateLimitedError struct{}

func (QueueRateLimitedError) Error() string     { return "too many queue state requests, try again later" }
func (QueueRateLimitedError) HTTPStatus() int   { return http.StatusTooManyRequests }
func (QueueRateLimitedError) ErrorCode() string { return "playback.rate_limited" }
