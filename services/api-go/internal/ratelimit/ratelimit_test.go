package ratelimit

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestAllow(t *testing.T) {
	mr := miniredis.RunT(t)
	now := time.Unix(1_800_000_000, 0)
	l := New(redis.NewClient(&redis.Options{Addr: mr.Addr()}), 60)
	l.now = func() time.Time { return now }
	ctx := context.Background()
	for i := 0; i < 60; i++ {
		if ok, err := l.Allow(ctx, "firm-a"); err != nil || !ok {
			t.Fatalf("call %d: ok=%v err=%v", i, ok, err)
		}
	}
	if ok, _ := l.Allow(ctx, "firm-a"); ok {
		t.Fatal("61st call allowed")
	}
	if ok, _ := l.Allow(ctx, "firm-b"); !ok {
		t.Fatal("other firm limited")
	}
	now = now.Add(61 * time.Second)
	if ok, _ := l.Allow(ctx, "firm-a"); !ok {
		t.Fatal("not reset after window")
	}
}
