package service

import "context"

type ConcurrencyLimit struct {
	slots chan struct{}
}

func NewDownloadLimiter(n int) *ConcurrencyLimit {
	return &ConcurrencyLimit{slots: make(chan struct{}, max(n, 1))}
}

func (l *ConcurrencyLimit) Acquire(ctx context.Context) error {
	if l.slots == nil {
		return nil
	}
	select {
	case l.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (l *ConcurrencyLimit) Release() {
	if l.slots == nil {
		return
	}
	<-l.slots
}
