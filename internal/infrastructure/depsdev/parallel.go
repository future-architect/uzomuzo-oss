package depsdev

import (
	"context"
	"sync"
)

// collectBounded runs fetch concurrently over keys with a bounded worker pool.
//
// The semaphore is acquired before launching each goroutine (select on both the
// semaphore channel and ctx.Done()), so context cancellation stops dispatch
// immediately without accumulating parked goroutines — this matches the
// project's learned concurrency rule ("acquire the semaphore before launching
// the goroutine, and select on ctx.Done() alongside the semaphore send to stop
// dispatch on cancellation").
//
// fetch receives the context and a single key. It returns:
//   - mapKey: the key under which v is stored in the result map
//   - v: the value to store
//   - ok: when false the entry is dropped (error or 404 semantics handled inside
//     fetch — caller controls skip-on-error vs store-with-error)
//
// collectBounded returns a non-nil map (possibly empty if all fetch calls drop).
func collectBounded[V any](
	ctx context.Context,
	keys []string,
	maxWorkers int,
	fetch func(ctx context.Context, key string) (mapKey string, v V, ok bool),
) map[string]V {
	results := make(map[string]V, len(keys))
	var mu sync.Mutex
	semaphore := make(chan struct{}, maxWorkers)
	var wg sync.WaitGroup

	for _, key := range keys {
		// Acquire semaphore or bail on context cancellation — before spawning
		// the goroutine so we never accumulate parked goroutines after cancel.
		acquired := false
		select {
		case semaphore <- struct{}{}:
			acquired = true
		case <-ctx.Done():
		}
		if !acquired {
			break
		}

		wg.Add(1)
		go func(k string) {
			defer wg.Done()
			defer func() { <-semaphore }()

			mapKey, v, ok := fetch(ctx, k)
			if !ok {
				return
			}

			mu.Lock()
			results[mapKey] = v
			mu.Unlock()
		}(key)
	}

	wg.Wait()
	return results
}
