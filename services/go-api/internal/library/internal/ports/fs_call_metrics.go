package ports

type FSCallMetrics interface {
	FSCallRefused()
	FSCallAbandoned()
}
