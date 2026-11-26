package clock

import "time"

type Func func() time.Time

func (f Func) UTC() time.Time {
	return f().UTC()
}
