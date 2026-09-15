package ports

// DBCallMetrics counts degradation at the catalog persistence boundary, so a
// wedged or overloaded database shows up as a number an operator can alert on
// instead of only as scattered request errors.
type DBCallMetrics interface {
	// DBCallTimedOut records one database operation cut off by the persistence
	// adapters' own per-call deadline (not by the caller's context).
	DBCallTimedOut()
}

// NoopDBCallMetrics returns a DBCallMetrics that records nothing. It is the
// default so the persistence adapters stay usable without a metrics backend.
func NoopDBCallMetrics() DBCallMetrics { return noopDBCallMetrics{} }

type noopDBCallMetrics struct{}

func (noopDBCallMetrics) DBCallTimedOut() {}
