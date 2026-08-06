package pledge

import (
	"context"
	"sync"
	"time"
)

// fakeRedis implements the Redis interface for tests. It also supports
// Flush, which is how test P5 is structured: Redis loses the key between two
// deliveries, and only the Postgres constraint can catch the duplicate.
type fakeRedis struct {
	mu   sync.Mutex
	keys map[string]time.Time
}

func newFakeRedis() *fakeRedis {
	return &fakeRedis{keys: make(map[string]time.Time)}
}

func (r *fakeRedis) SetNX(_ context.Context, key string, _ any, ttl time.Duration) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.keys[key]; ok {
		return false, nil
	}
	r.keys[key] = time.Now().Add(ttl)
	return true, nil
}

func (r *fakeRedis) Del(_ context.Context, keys ...string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var n int64
	for _, k := range keys {
		if _, ok := r.keys[k]; ok {
			delete(r.keys, k)
			n++
		}
	}
	return n, nil
}

func (r *fakeRedis) Flush() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.keys = make(map[string]time.Time)
}
