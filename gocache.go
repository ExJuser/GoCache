package GoCache

import (
	"context"
	"errors"
	"time"
)

const (
	minimumEntriesInShard = 10
)

type Response struct {
	EntryStatus RemoveReason
}

// RemoveReason 被删除的原因
type RemoveReason uint32

const (
	Expired = RemoveReason(1)
	NoSpace = RemoveReason(2)
	Deleted = RemoveReason(3)
)

type GoCache struct {
	shards     []*cacheShard
	lifeWindow uint64
	clock      clock
	hash       Hasher
	config     Config
	shardMask  uint64
	close      chan struct{}
}

func New(ctx context.Context, config Config) (*GoCache, error) {
	return newGoCache(ctx, config, &systemClock{})
}

func NewGoCache(config Config) (*GoCache, error) {
	return newGoCache(context.Background(), config, &systemClock{})
}

func newGoCache(ctx context.Context, config Config, clock clock) (*GoCache, error) {
	if !isPowerOfTwo(config.Shards) {
		//return nil, errors.New("shards number must be power of two")
		return nil, errors.New("分片数量必须是2的次方个")
	}
	if config.MaxEntrySize < 0 {
		return nil, errors.New("MaxEntrySize 必须为非负数")
	}
	if config.MaxEntriesInWindow < 0 {
		return nil, errors.New("MaxEntriesInWindow 必须为非负数")
	}
	if config.HardMaxCacheSize < 0 {
		return nil, errors.New("HardMaxCacheSize 必须为非负数")
	}

	lifeWindowSeconds := uint64(config.LifeWindow.Seconds())
	//默认一秒钟清理一次 此时过期时间必须大于一秒 否则每次清理都需要清空全部
	if config.CleanWindow > 0 && lifeWindowSeconds == 0 {
		return nil, errors.New("LifeWindow必须大于等于CleanWindow")
	}

	if config.Hasher == nil {
		config.Hasher = newDefaultHasher()
	}

	cache := &GoCache{
		shards:     make([]*cacheShard, config.Shards),
		lifeWindow: lifeWindowSeconds,
		clock:      clock,
		hash:       config.Hasher,
		config:     config,
		shardMask:  uint64(config.Shards - 1), //位运算(替代取模)得到所在分片时使用
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

	//启动异步协程定时清理缓存
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

func (c *GoCache) Close() error {
	close(c.close)
	return nil
}

// Get 获取缓存
func (c *GoCache) Get(key string) ([]byte, error) {
	hashedKey := c.hash.Sum64(key)
	shard := c.getShard(hashedKey)
	return shard.get(key, hashedKey)
}

func (c *GoCache) GetWithInfo(key string) ([]byte, Response, error) {
	hashedKey := c.hash.Sum64(key)
	shard := c.getShard(hashedKey)
	return shard.getWithInfo(key, hashedKey)
}

// Set 设置缓存
func (c *GoCache) Set(key string, entry []byte) error {
	hashedKey := c.hash.Sum64(key)
	shard := c.getShard(hashedKey)
	return shard.set(key, hashedKey, entry)
}

func (c *GoCache) Append(key string, entry []byte) error {
	hashedKey := c.hash.Sum64(key)
	shard := c.getShard(hashedKey)
	return shard.append(key, hashedKey, entry)
}

// Delete 删除缓存
func (c *GoCache) Delete(key string) error {
	hashedKey := c.hash.Sum64(key)
	shard := c.getShard(hashedKey)
	return shard.del(hashedKey)
}

// Reset 清空全部缓存
func (c *GoCache) Reset() error {
	//遍历每一个分片
	for _, shard := range c.shards {
		shard.reset(c.config)
	}
	return nil
}

// ResetStats 清空统计数据
func (c *GoCache) ResetStats() error {
	//遍历每一个分片
	for _, shard := range c.shards {
		shard.resetStats()
	}
	return nil
}

// Len 长度定义为每一个分片的hashmap的len之和 即共有多少条有效条目
func (c *GoCache) Len() int {
	var length int
	for _, shard := range c.shards {
		length += shard.len()
	}
	return length
}

func (c *GoCache) Capacity() int {
	var length int
	for _, shard := range c.shards {
		length += shard.capacity()
	}
	return length
}

// Stats 遍历每一个分片 累加stats信息
func (c *GoCache) Stats() Stats {
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

func (c *GoCache) KeyMetadata(key string) Metadata {
	hashedKey := c.hash.Sum64(key)
	shard := c.getShard(hashedKey)
	return shard.getKeyMetadataWithLock(hashedKey)
}

func (c *GoCache) Iterator() *EntryInfoIterator {
	return newIterator(c)
}

func (c *GoCache) onEvict(oldestEntry []byte, currentTimestamp uint64, evict func(reason RemoveReason) error) bool {
	oldestTimestamp := readTimestampFromEntry(oldestEntry)
	//插入时间大于当前时间 实际上是在触发了过期清理之后才添加的
	if oldestTimestamp > currentTimestamp {
		return false
	}
	//当前时间-插入时间超过了清理窗口 认为已经过期
	if currentTimestamp-oldestTimestamp > c.lifeWindow {
		evict(Expired)
		return true
	}
	return false
}

func (c *GoCache) cleanUp(currentTimestamp uint64) {
	for _, shard := range c.shards {
		shard.cleanUp(currentTimestamp)
	}
}

// 由于限制了分片数量为2的次方 此处取模操作可以换成位运算
func (c *GoCache) getShard(hashedKey uint64) (shard *cacheShard) {
	return c.shards[hashedKey&c.shardMask]
}

func (c *GoCache) providedOnRemove(wrappedEntry []byte, reason RemoveReason) {
	c.config.OnRemove(readKeyFromEntry(wrappedEntry), readEntry(wrappedEntry))
}

func (c *GoCache) providedOnRemoveWithReason(wrappedEntry []byte, reason RemoveReason) {
	if c.config.onRemoveFilter == 0 || (1<<uint(reason))&c.config.onRemoveFilter > 0 {
		c.config.OnRemoveWithReason(readKeyFromEntry(wrappedEntry), readEntry(wrappedEntry), reason)
	}
}

func (c *GoCache) notProvidedOnRemove(wrappedEntry []byte, reason RemoveReason) {
}

func (c *GoCache) providedOnRemoveWithMetadata(wrappedEntry []byte, reason RemoveReason) {
	key := readKeyFromEntry(wrappedEntry)

	hashedKey := c.hash.Sum64(key)
	shard := c.getShard(hashedKey)
	c.config.OnRemoveWithMetadata(key, readEntry(wrappedEntry), shard.getKeyMetadata(hashedKey))
}

func isPowerOfTwo(number int) bool {
	return (number != 0) && (number&(number-1)) == 0
}
