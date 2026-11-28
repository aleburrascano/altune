package ports

type DBCallMetrics interface {
	DBCallTimedOut()
}
