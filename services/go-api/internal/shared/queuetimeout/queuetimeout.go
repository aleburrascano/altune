package queuetimeout

import (
	"errors"
	"time"
)

var Err = errors.New("provider rate-limit queue timeout")

const MaxWait = 8 * time.Second
