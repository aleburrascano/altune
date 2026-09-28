package ports

type DBCallMetrics interface {
	DBCallTimedOut()
}

func NoopDBCallMetrics() DBCallMetrics { return noopDBCallMetrics{} }

type noopDBCallMetrics struct{}

func (noopDBCallMetrics) DBCallTimedOut() {}
