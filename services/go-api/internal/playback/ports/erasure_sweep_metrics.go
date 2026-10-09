package ports

type ErasureSweepMetrics interface {
	SweepIdle()
	QueueStateErased(n int)
}

func NoopErasureSweepMetrics() ErasureSweepMetrics { return noopErasureSweepMetrics{} }

type noopErasureSweepMetrics struct{}

func (noopErasureSweepMetrics) SweepIdle()           {}
func (noopErasureSweepMetrics) QueueStateErased(int) {}
