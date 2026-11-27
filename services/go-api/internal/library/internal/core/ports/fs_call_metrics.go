package ports

type FSCallMetrics interface {
	FSCallRefused()
	FSCallAbandoned()
}

func NoopFSCallMetrics() FSCallMetrics { return noopFSCallMetrics{} }

type noopFSCallMetrics struct{}

func (noopFSCallMetrics) FSCallRefused()   {}
func (noopFSCallMetrics) FSCallAbandoned() {}
