package ports

type QueueStateMetrics interface {
	CorruptStoredState()
	QueueStateOpTimedOut()
	QueueStateOpFailedTransient()
	QueueStateStaleWrite()
}

func NoopQueueStateMetrics() QueueStateMetrics { return noopQueueStateMetrics{} }

type noopQueueStateMetrics struct{}

func (noopQueueStateMetrics) CorruptStoredState()          {}
func (noopQueueStateMetrics) QueueStateOpTimedOut()        {}
func (noopQueueStateMetrics) QueueStateOpFailedTransient() {}
func (noopQueueStateMetrics) QueueStateStaleWrite()        {}
