package GoCache

import (
	"context"
	"errors"
	"time"
)

const (
	minimumEntriesInShard = 10
)

type BigCache struct {
	shards     []*cacheShard
	lifeWindow uint64
	clock      clock
	hash       Hasher
	config     Config
	shardMask  uint64
	close      chan struct{}
}

type Response struct {
	EntryStatus RemoveReason
}

type RemoveReason uint32

const (
	Expired = RemoveReason(1)
	NoSpace = RemoveReason(2)
	Deleted = RemoveReason(3)
)

func New(ctx context.Context, config Config) (*BigCache, error) {
	return newBigCache(ctx, config, &systemClock{})
}

func NewBigCache(config Config) (*BigCache, error) {
	return newBigCache(context.Background(), config, &systemClock{})
}

func newBigCache(ctx context.Context, config Config, clock clock) (*BigCache, error) {
	if !isPowerOfTwo(config.Shards) {
		return nil, errors.New("Shards number must be power of two")
	}
	if config.MaxEntrySize < 0 {
		return nil, errors.New("MaxEntrySize must be >= 0")
	}
	if config.MaxEntriesInWindow < 0 {
		return nil, errors.New("MaxEntriesInWindow must be >= 0")
	}
	if config.HardMaxCacheSize < 0 {
		return nil, errors.New("HardMaxCacheSize must be >= 0")
	}

	lifeWindowSeconds := uint64(config.LifeWindow.Seconds())
	if config.CleanWindow > 0 && lifeWindowSeconds == 0 {
		return nil, errors.New("LifeWindow must be >= 1s when CleanWindow is set")
	}

	if config.Hasher == nil {
		config.Hasher = newDefaultHasher()
	}

	cache := &BigCache{
		shards:     make([]*cacheShard, config.Shards),
		lifeWindow: lifeWindowSeconds,
		clock:      clock,
		hash:       config.Hasher,
		config:     config,
		shardMask:  uint64(config.Shards - 1),
		close:      make(chan struct{}),
	}

	var onRemove func(wrappedEntry []byte, reason RemoveReason)
	if config.OnRemoveWithMetadata != nil {
		onRemove = cache.providedOnRemoveWithMetadata
	} else if config.OnRemove != nil {
		onRemove = cache.providedOnRemove
	} else if config.OnRemoveWithReason != nil {
		onRemove = cache.providedOnRemoveWithReason
	} else {
		onRemove = cache.notProvidedOnRemove
	}

	for i := 0; i < config.Shards; i++ {
		cache.shards[i] = initNewShard(config, onRemove, clock)
	}

	if config.CleanWindow > 0 {
		go func() {
			ticker := time.NewTicker(config.CleanWindow)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case t := <-ticker.C:
					cache.cleanUp(uint64(t.Unix()))
				case <-cache.close:
					return
				}
			}
		}()
	}

	return cache, nil
}

func (c *BigCache) Close() error {
	close(c.close)
	return nil
}

func (c *BigCache) Get(key string) ([]byte, error) {
	hashedKey := c.hash.Sum64(key)
	shard := c.getShard(hashedKey)
	return shard.get(key, hashedKey)
}

func (c *BigCache) GetWithInfo(key string) ([]byte, Response, error) {
	hashedKey := c.hash.Sum64(key)
	shard := c.getShard(hashedKey)
	return shard.getWithInfo(key, hashedKey)
}

func (c *BigCache) Set(key string, entry []byte) error {
	hashedKey := c.hash.Sum64(key)
	shard := c.getShard(hashedKey)
	return shard.set(key, hashedKey, entry)
}

func (c *BigCache) Append(key string, entry []byte) error {
	hashedKey := c.hash.Sum64(key)
	shard := c.getShard(hashedKey)
	return shard.append(key, hashedKey, entry)
}

func (c *BigCache) Delete(key string) error {
	hashedKey := c.hash.Sum64(key)
	shard := c.getShard(hashedKey)
	return shard.del(hashedKey)
}

func (c *BigCache) Reset() error {
	for _, shard := range c.shards {
		shard.reset(c.config)
	}
	return nil
}

func (c *BigCache) ResetStats() error {
	for _, shard := range c.shards {
		shard.resetStats()
	}
	return nil
}

func (c *BigCache) Len() int {
	var len int
	for _, shard := range c.shards {
		len += shard.len()
	}
	return len
}

func (c *BigCache) Capacity() int {
	var len int
	for _, shard := range c.shards {
		len += shard.capacity()
	}
	return len
}

func (c *BigCache) Stats() Stats {
	var s Stats
	for _, shard := range c.shards {
		tmp := shard.getStats()
		s.Hits += tmp.Hits
		s.Misses += tmp.Misses
		s.DelHits += tmp.DelHits
		s.DelMisses += tmp.DelMisses
		s.Collisions += tmp.Collisions
	}
	return s
}

func (c *BigCache) KeyMetadata(key string) Metadata {
	hashedKey := c.hash.Sum64(key)
	shard := c.getShard(hashedKey)
	return shard.getKeyMetadataWithLock(hashedKey)
}

func (c *BigCache) Iterator() *EntryInfoIterator {
	return newIterator(c)
}

func (c *BigCache) onEvict(oldestEntry []byte, currentTimestamp uint64, evict func(reason RemoveReason) error) bool {
	oldestTimestamp := readTimestampFromEntry(oldestEntry)
	if currentTimestamp < oldestTimestamp {
		return false
	}
	if currentTimestamp-oldestTimestamp > c.lifeWindow {
		evict(Expired)
		return true
	}
	return false
}

func (c *BigCache) cleanUp(currentTimestamp uint64) {
	for _, shard := range c.shards {
		shard.cleanUp(currentTimestamp)
	}
}

func (c *BigCache) getShard(hashedKey uint64) (shard *cacheShard) {
	return c.shards[hashedKey&c.shardMask]
}

func (c *BigCache) providedOnRemove(wrappedEntry []byte, reason RemoveReason) {
	c.config.OnRemove(readKeyFromEntry(wrappedEntry), readEntry(wrappedEntry))
}

func (c *BigCache) providedOnRemoveWithReason(wrappedEntry []byte, reason RemoveReason) {
	if c.config.onRemoveFilter == 0 || (1<<uint(reason))&c.config.onRemoveFilter > 0 {
		c.config.OnRemoveWithReason(readKeyFromEntry(wrappedEntry), readEntry(wrappedEntry), reason)
	}
}

func (c *BigCache) notProvidedOnRemove(wrappedEntry []byte, reason RemoveReason) {
}

func (c *BigCache) providedOnRemoveWithMetadata(wrappedEntry []byte, reason RemoveReason) {
	key := readKeyFromEntry(wrappedEntry)

	hashedKey := c.hash.Sum64(key)
	shard := c.getShard(hashedKey)
	c.config.OnRemoveWithMetadata(key, readEntry(wrappedEntry), shard.getKeyMetadata(hashedKey))
}

func isPowerOfTwo(number int) bool {
	return (number != 0) && (number&(number-1)) == 0
}
