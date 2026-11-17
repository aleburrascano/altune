package goapi

import (
	goapihttp "altune/overseer/internal/goapi/internal/adapters/goapihttp"
	"os"
	"sync"
)

var (
	sharedOnce   sync.Once
	sharedSource TokenSource
)

func SharedTokenSource() TokenSource {
	sharedOnce.Do(func() { sharedSource = goapihttp.SelectTokenSource(os.Getenv) })
	return sharedSource
}
