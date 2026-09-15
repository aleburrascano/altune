package ports

import "errors"

// ErrDBTransient marks a persistence failure the catalog persistence adapters
// classified as transient: the per-call deadline fired, the connection could
// not be established or was lost, or the server reported a connection-class,
// resource or shutdown condition. The operation may succeed if retried, so a
// service maps it to a retryable response rather than an internal error. The
// adapter wraps the original error alongside it, so errors.Is/As still reach
// the underlying cause.
var ErrDBTransient = errors.New("catalog database temporarily unavailable")
