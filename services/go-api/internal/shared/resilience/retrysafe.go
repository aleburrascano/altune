package resilience

import "context"

type markKey struct{}

func MarkRetrySafe(ctx context.Context) context.Context {
	return context.WithValue(ctx, markKey{}, true)
}

func IsRetrySafe(ctx context.Context) bool {
	marked, _ := ctx.Value(markKey{}).(bool)
	return marked
}
