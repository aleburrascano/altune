package retrysafe

import "context"

type markKey struct{}

func Mark(ctx context.Context) context.Context {
	return context.WithValue(ctx, markKey{}, true)
}

func Marked(ctx context.Context) bool {
	marked, _ := ctx.Value(markKey{}).(bool)
	return marked
}
