package resilience

import (
	"errors"
	"time"
)

var ErrQueueTimeout = errors.New("provider rate-limit queue timeout")

const MaxQueueWait = 8 * time.Second
