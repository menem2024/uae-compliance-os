// Package ratelimit implements a per-firm fixed-window limiter on Valkey.
package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Limiter allows at most limit calls per firm per wall-clock minute.
type Limiter struct {
	rdb    *redis.Client
	limit  int64
	prefix string
	now    func() time.Time
}

// New returns a limiter allowing perMinute calls per firm per minute.
func New(rdb *redis.Client, perMinute int64) *Limiter {
	return &Limiter{rdb: rdb, limit: perMinute, prefix: "rl:", now: time.Now}
}

// NewNamed returns a limiter with its own key space rl:<name>:<firm>:<minute>, so its budget is
// never shared with New's or another name's.
func NewNamed(rdb *redis.Client, name string, perMinute int64) *Limiter {
	return &Limiter{rdb: rdb, limit: perMinute, prefix: "rl:" + name + ":", now: time.Now}
}

// Allow implements a fixed one-minute window per firm.
func (l *Limiter) Allow(ctx context.Context, firmID string) (bool, error) {
	key := fmt.Sprintf("%s%s:%d", l.prefix, firmID, l.now().Unix()/60)
	pipe := l.rdb.TxPipeline()
	incr := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, 61*time.Second)
	if _, err := pipe.Exec(ctx); err != nil {
		return false, fmt.Errorf("rate limit %s: %w", key, err)
	}
	return incr.Val() <= l.limit, nil
}
