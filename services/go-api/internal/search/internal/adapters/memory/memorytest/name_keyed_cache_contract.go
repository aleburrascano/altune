package memorytest

import (
	"context"
	"testing"
)

type NameKeyedCacheUnderTest[T any] interface {
	Get(ctx context.Context, nameKey string) (T, bool, error)
	Set(ctx context.Context, nameKey string, v T) error
	GetNegative(ctx context.Context, nameKey string) (bool, error)
	SetNegative(ctx context.Context, nameKey string) error
}

func NameKeyedCacheContract[T any](t *testing.T, newCache func(t *testing.T) NameKeyedCacheUnderTest[T], sample, other T, same func(a, b T) bool) {
	t.Helper()
	contracts := map[string]func(*testing.T, NameKeyedCacheUnderTest[T]){
		"a name never stored is a miss and not negative": func(t *testing.T, c NameKeyedCacheUnderTest[T]) {
			_, ok, err := c.Get(t.Context(), "nobody")
			neg, negErr := c.GetNegative(t.Context(), "nobody")
			if err != nil || negErr != nil || ok || neg {
				t.Errorf("Get ok=%v err=%v, GetNegative=%v err=%v, want a plain miss", ok, err, neg, negErr)
			}
		},
		"a stored value is read back": func(t *testing.T, c NameKeyedCacheUnderTest[T]) {
			if err := c.Set(t.Context(), "daft punk", sample); err != nil {
				t.Fatalf("Set: %v", err)
			}
			got, ok, err := c.Get(t.Context(), "daft punk")
			if err != nil || !ok || !same(got, sample) {
				t.Errorf("Get = (%v, %v, %v), want the stored value", got, ok, err)
			}
		},
		"setting a name again replaces the value": func(t *testing.T, c NameKeyedCacheUnderTest[T]) {
			_ = c.Set(t.Context(), "daft punk", sample)
			if err := c.Set(t.Context(), "daft punk", other); err != nil {
				t.Fatalf("Set again: %v", err)
			}
			got, ok, _ := c.Get(t.Context(), "daft punk")
			if !ok || !same(got, other) {
				t.Errorf("Get = (%v, %v), want the second value", got, ok)
			}
		},
		"a name stored under one key is not found under another": func(t *testing.T, c NameKeyedCacheUnderTest[T]) {
			_ = c.Set(t.Context(), "daft punk", sample)
			if _, ok, _ := c.Get(t.Context(), "justice"); ok {
				t.Error("Get(justice) hit a value stored for daft punk")
			}
		},
		"a negative is remembered for its name only": func(t *testing.T, c NameKeyedCacheUnderTest[T]) {
			if err := c.SetNegative(t.Context(), "nobody"); err != nil {
				t.Fatalf("SetNegative: %v", err)
			}
			neg, err := c.GetNegative(t.Context(), "nobody")
			other, _ := c.GetNegative(t.Context(), "somebody")
			if err != nil || !neg || other {
				t.Errorf("GetNegative(nobody) = %v, err %v; GetNegative(somebody) = %v, want true, false", neg, err, other)
			}
		},
		"a negative does not hide a stored value and a value does not make a negative": func(t *testing.T, c NameKeyedCacheUnderTest[T]) {
			_ = c.Set(t.Context(), "daft punk", sample)
			if neg, _ := c.GetNegative(t.Context(), "daft punk"); neg {
				t.Error("a stored name reads as negative")
			}
			_ = c.SetNegative(t.Context(), "nobody")
			if _, ok, _ := c.Get(t.Context(), "nobody"); ok {
				t.Error("a negative name reads as a stored value")
			}
		},
	}
	for name, contract := range contracts {
		t.Run(name, func(t *testing.T) { contract(t, newCache(t)) })
	}
}
