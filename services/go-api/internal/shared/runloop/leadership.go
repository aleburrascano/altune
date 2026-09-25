package runloop

import "context"

type LeadershipScope func(parent context.Context) (ctx context.Context, release context.CancelFunc, ok bool)

func EveryPassLeads(parent context.Context) (context.Context, context.CancelFunc, bool) {
	ctx, cancel := context.WithCancel(parent)
	return ctx, cancel, true
}
