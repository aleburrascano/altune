package service

import (
	ports "altune/go-api/internal/playback/internal"
	"errors"
	"fmt"
	"time"
)

type QueueService struct {
	repo       ports.QueueStateRepository
	nowPlaying ports.NowPlayingReader
	now        func() time.Time
}

var ErrQueueClockRequired = errors.New("queue: the queue service requires a clock")

func NewQueueService(repo ports.QueueStateRepository, nowPlaying ports.NowPlayingReader, now func() time.Time) (*QueueService, error) {
	if now == nil {
		return nil, fmt.Errorf("NewQueueService: %w", ErrQueueClockRequired)
	}
	return &QueueService{repo: repo, nowPlaying: nowPlaying, now: now}, nil
}

func MustNewQueueService(repo ports.QueueStateRepository, nowPlaying ports.NowPlayingReader, now func() time.Time) *QueueService {
	s, err := NewQueueService(repo, nowPlaying, now)
	if err != nil {
		panic(err)
	}
	return s
}
