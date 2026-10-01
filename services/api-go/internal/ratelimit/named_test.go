package ratelimit

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestNamedLimitersHaveSeparateKeysAndBudgets(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	now := time.Unix(1_800_000_000, 0)
	shared, uploads := New(rdb, 1), NewNamed(rdb, "uploads", 2)
	shared.now, uploads.now = func() time.Time { return now }, func() time.Time { return now }
	ctx := context.Background()
	if ok, _ := shared.Allow(ctx, "f"); !ok {
		t.Fatal("shared first call denied")
	}
	for i := 0; i < 2; i++ {
		if ok, _ := uploads.Allow(ctx, "f"); !ok {
			t.Fatalf("uploads call %d denied: budgets are shared", i)
		}
	}
	if ok, _ := uploads.Allow(ctx, "f"); ok {
		t.Fatal("uploads over its limit")
	}
	minute := now.Unix() / 60
	for _, key := range []string{"rl:f:" + itoa(minute), "rl:uploads:f:" + itoa(minute)} {
		if !mr.Exists(key) {
			t.Errorf("missing key %s (keys: %v)", key, mr.Keys())
		}
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
