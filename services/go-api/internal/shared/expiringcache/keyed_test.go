package expiringcache_test

import (
	"altune/go-api/internal/shared/expiringcache"
	"strconv"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func newKeyed(maxKeys int, c *clock) *expiringcache.Keyed[string, *rate.Limiter] {
	return expiringcache.NewKeyed[string, *rate.Limiter](time.Hour, maxKeys, c.now)
}

func limiterFor(k *expiringcache.Keyed[string, *rate.Limiter], key string) *rate.Limiter {
	return k.Get(key, func() *rate.Limiter { return rate.NewLimiter(rate.Every(time.Hour), 2) })
}

func TestKeyed_ReturnsTheSameLimiterForAKey(t *testing.T) {
	k := newKeyed(10, newClock())

	first := limiterFor(k, "a")
	if limiterFor(k, "a") != first {
		t.Fatal("two Gets of one key built two limiters")
	}
	if limiterFor(k, "a") == limiterFor(k, "b") {
		t.Fatal("two keys shared a limiter")
	}
}

func TestKeyed_DropsAKeyUntouchedForTwoIdlePeriods(t *testing.T) {
	c := newClock()
	k := newKeyed(10, c)
	first := limiterFor(k, "a")

	c.advance(time.Hour)
	limiterFor(k, "other")
	c.advance(time.Hour)
	limiterFor(k, "other")

	if limiterFor(k, "a") == first {
		t.Fatal("a key idle for two periods kept its old limiter")
	}
}

func TestKeyed_KeepsAKeyTouchedInsideOneIdlePeriod(t *testing.T) {
	c := newClock()
	k := newKeyed(10, c)
	first := limiterFor(k, "a")

	c.advance(time.Hour)
	limiterFor(k, "other")
	limiterFor(k, "a")
	c.advance(time.Hour)
	limiterFor(k, "other")

	if limiterFor(k, "a") != first {
		t.Fatal("a key touched in the previous period lost its limiter")
	}
}

func TestKeyed_StaysBoundedAndEvictsTheLeastPenalisedFirst(t *testing.T) {
	c := newClock()
	k := newKeyed(20, c)
	spent := limiterFor(k, "spent")
	spent.AllowN(c.now(), 2)

	for i := range 500 {
		limiterFor(k, "k"+strconv.Itoa(i))
	}

	if got := k.Len(); got > 20 {
		t.Fatalf("tracked keys = %d, want at most 20", got)
	}
	if limiterFor(k, "spent") != spent {
		t.Fatal("a spent limiter was evicted while unpenalised ones were available")
	}
}

func TestKeyed_PrunesRefilledLimitersBeforeEvictingPenalisedOnes(t *testing.T) {
	c := newClock()
	k := newKeyed(2, c)
	idle := limiterFor(k, "idle")
	idle.AllowN(c.now(), 1)
	c.advance(2 * time.Hour)
	spent := limiterFor(k, "spent")
	spent.AllowN(c.now(), 2)

	limiterFor(k, "new")

	if limiterFor(k, "spent") != spent {
		t.Fatal("a penalised limiter was evicted in favour of pruning a refilled one")
	}
}
