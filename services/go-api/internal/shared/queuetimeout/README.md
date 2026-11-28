Contracts: `queuetimeout` owns the one error that says an outbound request was refused because it would have waited too long for its provider's rate limit.

It exists so the module that paces the call and the modules that decide what a refusal means agree on it without importing each other.

`platform/outbound` returns an error that wraps `queuetimeout.Err` and `context.DeadlineExceeded`. `search` aliases it as `ErrProviderRateLimitQueueTimeout`, so its circuit breaker, fan-out and metrics treat the refusal as a shed request and never as a provider failure. It lives in `shared` because a feature never imports a platform module.
