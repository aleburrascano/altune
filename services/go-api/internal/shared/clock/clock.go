package clock

import "time"

type Func func() time.Time

func Now() time.Time {
	return time.Now()
}

func (f Func) UTC() time.Time {
	return f().UTC()
}
