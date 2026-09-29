package service

import "context"

type DownloadLimiter struct {
	slots chan struct{}
}

func NewDownloadLimiter(n int) *DownloadLimiter {
	return &DownloadLimiter{slots: make(chan struct{}, max(n, 1))}
}

func (l *DownloadLimiter) Acquire(ctx context.Context) error {
	if l == nil {
		return nil
	}
	select {
	case l.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (l *DownloadLimiter) Release() {
	if l == nil {
		return
	}
	<-l.slots
}
