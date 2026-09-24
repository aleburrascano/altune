package shell_test

import "altune/overseer/internal/core"

func (f fixedRegistry) Get(id string) (core.Bucket, bool) {
	for _, b := range f.buckets {
		if b.Meta().ID == id {
			return b, true
		}
	}
	return nil, false
}
