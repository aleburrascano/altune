package goapi

import (
	goapihttp "altune/overseer/internal/goapi/internal/adapters/goapihttp"
	"sync"
)

var (
	sharedOnce   sync.Once
	sharedSource TokenSource
)

func SharedTokenSource(getenv func(string) string) TokenSource {
	sharedOnce.Do(func() { sharedSource = goapihttp.SelectTokenSource(getenv) })
	return sharedSource
}
