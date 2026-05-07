package resolver

import (
	"sync"

	"github.com/philtechs-org/phi/internal/registry"
)

// packumentCache fetches packuments from the registry, deduplicates
// concurrent requests for the same name, and exposes a Prefetch method
// for warming the cache in the background.
//
// The resolver runs BFS sequentially for determinism, but each iteration
// only needs ~1 ms of CPU plus a packument fetch (~50-200 ms over the
// network). Prefetching children's packuments while we process the
// parent collapses serial latency into parallel I/O.
type packumentCache struct {
	client *registry.Client
	sem    chan struct{}

	mu       sync.Mutex
	fetched  map[string]*registry.Packument
	errs     map[string]error
	fetching map[string]chan struct{}
}

func newPackumentCache(client *registry.Client, maxConcurrent int) *packumentCache {
	if maxConcurrent < 1 {
		maxConcurrent = 16
	}
	return &packumentCache{
		client:   client,
		sem:      make(chan struct{}, maxConcurrent),
		fetched:  map[string]*registry.Packument{},
		errs:     map[string]error{},
		fetching: map[string]chan struct{}{},
	}
}

// Get returns the packument for name. Blocks if a fetch is already in
// flight; otherwise fetches synchronously.
func (pc *packumentCache) Get(name string) (*registry.Packument, error) {
	pc.mu.Lock()
	if p, ok := pc.fetched[name]; ok {
		err := pc.errs[name]
		pc.mu.Unlock()
		return p, err
	}
	if ch, ok := pc.fetching[name]; ok {
		pc.mu.Unlock()
		<-ch
		pc.mu.Lock()
		p, err := pc.fetched[name], pc.errs[name]
		pc.mu.Unlock()
		return p, err
	}
	ch := make(chan struct{})
	pc.fetching[name] = ch
	pc.mu.Unlock()

	pc.sem <- struct{}{}
	p, err := pc.client.FetchPackument(name)
	<-pc.sem

	pc.mu.Lock()
	pc.fetched[name] = p
	pc.errs[name] = err
	delete(pc.fetching, name)
	pc.mu.Unlock()
	close(ch)

	return p, err
}

// Prefetch kicks off fetches for any names that aren't already cached or
// in flight. Non-blocking; results land in the cache when ready.
func (pc *packumentCache) Prefetch(names []string) {
	pc.mu.Lock()
	var toFetch []string
	for _, n := range names {
		if _, hit := pc.fetched[n]; hit {
			continue
		}
		if _, infl := pc.fetching[n]; infl {
			continue
		}
		ch := make(chan struct{})
		pc.fetching[n] = ch
		toFetch = append(toFetch, n)
	}
	pc.mu.Unlock()

	for _, n := range toFetch {
		go func(name string) {
			pc.sem <- struct{}{}
			p, err := pc.client.FetchPackument(name)
			<-pc.sem

			pc.mu.Lock()
			pc.fetched[name] = p
			pc.errs[name] = err
			ch := pc.fetching[name]
			delete(pc.fetching, name)
			pc.mu.Unlock()
			close(ch)
		}(n)
	}
}
