package GoCache

import (
	"errors"
	"github.com/ExJuser/GoCache/queue"
	"sync"
	"sync/atomic"
)

type onRemoveCallback func(wrappedEntry []byte, reason RemoveReason)

// Metadata 包含了一条记录的详细信息
type Metadata struct {
	// RequestCount 现阶段只包含了请求次数
	RequestCount uint32
}

type cacheShard struct {
	//读写分离结构 hashmap存储一条记录的索引
	hashmap map[uint64]uint64
	//entries 存储底层真实数据
	entries queue.BytesQueue

	//Go自带的map不是并发安全的 读写锁保护hashmap的并发访问
	lock        sync.RWMutex
	entryBuffer []byte
	onRemove    onRemoveCallback

	isVerbose    bool
	statsEnabled bool
	logger       Logger
	clock        clock
	lifeWindow   uint64

	hashmapStats map[uint64]uint32
	stats        Stats
	cleanEnabled bool
}

// 相比于get多返回了一个resp 包含了 RemoveReason
func (s *cacheShard) getWithInfo(key string, hashedKey uint64) (entry []byte, resp Response, err error) {
	currentTime := uint64(s.clock.Epoch())
	s.lock.RLock()
	wrappedEntry, err := s.getWrappedEntry(hashedKey)
	if err != nil {
		s.lock.RUnlock()
		return nil, resp, err
	}
	if entryKey := readKeyFromEntry(wrappedEntry); key != entryKey {
		s.lock.RUnlock()
		s.collision()
		if s.isVerbose {
			s.logger.Printf("Collision detected. Both %q and %q have the same hash %x", key, entryKey, hashedKey)
		}
		return nil, resp, ErrEntryNotFound
	}

	entry = readEntry(wrappedEntry)
	if s.isExpired(wrappedEntry, currentTime) {
		resp.EntryStatus = Expired
	}
	s.lock.RUnlock()
	s.hit(hashedKey)
	return entry, resp, nil
}

// key是键值对的Key hashKey是对应哈希分片映射到的shard的Key
func (s *cacheShard) get(key string, hashedKey uint64) ([]byte, error) {
	s.lock.RLock()
	wrappedEntry, err := s.getWrappedEntry(hashedKey)
	if err != nil {
		s.lock.RUnlock()
		return nil, err
	}
	//拆解 wrapperEntry包括了一条记录的完整信息：插入时间戳、hash值、长度、key、value等
	//如果 key != entryKey 说明发生了碰撞；gocache 不考虑缓存冲突时的解决方案
	if entryKey := readKeyFromEntry(wrappedEntry); key != entryKey {
		s.lock.RUnlock()
		s.collision()
		if s.isVerbose {
			s.logger.Printf("Collision detected. Both %q and %q have the same hash %x", key, entryKey, hashedKey)
		}
		return nil, ErrEntryNotFound
	}

	entry := readEntry(wrappedEntry)
	s.lock.RUnlock()
	s.hit(hashedKey)

	return entry, nil
}

func (s *cacheShard) getWrappedEntry(hashedKey uint64) ([]byte, error) {
	//找到在 BytesQueue 中的索引
	itemIndex := s.hashmap[hashedKey]

	//没找到 缓存Miss
	if itemIndex == 0 {
		s.miss()
		return nil, ErrEntryNotFound
	}

	//在 BytesQueue 中寻找并返回
	//wrapperEntry包括了一条记录的完整信息：插入时间戳、hash值、长度、key、value等
	wrappedEntry, err := s.entries.Get(int(itemIndex))
	if err != nil {
		s.miss()
		return nil, err
	}

	return wrappedEntry, err
}

func (s *cacheShard) getValidWrapEntry(key string, hashedKey uint64) ([]byte, error) {
	wrappedEntry, err := s.getWrappedEntry(hashedKey)
	if err != nil {
		return nil, err
	}

	if !compareKeyFromEntry(wrappedEntry, key) {
		s.collision()
		if s.isVerbose {
			s.logger.Printf("Collision detected. Both %q and %q have the same hash %x", key, readKeyFromEntry(wrappedEntry), hashedKey)
		}

		return nil, ErrEntryNotFound
	}
	s.hitWithoutLock(hashedKey)

	return wrappedEntry, nil
}

func (s *cacheShard) set(key string, hashedKey uint64, entry []byte) error {
	currentTimestamp := uint64(s.clock.Epoch())

	s.lock.Lock()

	//对应的hashedKey在hashmap中已经存在 之前已经添加过
	if previousIndex := s.hashmap[hashedKey]; previousIndex != 0 {
		//找到之前添加的对应条目
		if previousEntry, err := s.entries.Get(int(previousIndex)); err == nil {
			resetHashFromEntry(previousEntry)
			//并且将其从hashmap中也删除 索引和数据都删除
			delete(s.hashmap, hashedKey)
		}
	}

	if !s.cleanEnabled {
		if oldestEntry, err := s.entries.Peek(); err == nil {
			s.onEvict(oldestEntry, currentTimestamp, s.removeOldestEntry)
		}
	}

	//将要插入的键值对包装为一个条目
	w := wrapEntry(currentTimestamp, hashedKey, key, entry, &s.entryBuffer)

	for {
		//插入成功 在哈希表中也插入对应的索引
		if index, err := s.entries.Push(w); err == nil {
			s.hashmap[hashedKey] = uint64(index)
			s.lock.Unlock()
			return nil
		}
		if s.removeOldestEntry(NoSpace) != nil {
			s.lock.Unlock()
			return errors.New("entry is bigger than max shard size")
		}
	}
}

func (s *cacheShard) addNewWithoutLock(key string, hashedKey uint64, entry []byte) error {
	currentTimestamp := uint64(s.clock.Epoch())

	if !s.cleanEnabled {
		if oldestEntry, err := s.entries.Peek(); err == nil {
			s.onEvict(oldestEntry, currentTimestamp, s.removeOldestEntry)
		}
	}

	w := wrapEntry(currentTimestamp, hashedKey, key, entry, &s.entryBuffer)

	for {
		if index, err := s.entries.Push(w); err == nil {
			s.hashmap[hashedKey] = uint64(index)
			return nil
		}
		if s.removeOldestEntry(NoSpace) != nil {
			return errors.New("entry is bigger than max shard size")
		}
	}
}

func (s *cacheShard) setWrappedEntryWithoutLock(currentTimestamp uint64, w []byte, hashedKey uint64) error {
	if previousIndex := s.hashmap[hashedKey]; previousIndex != 0 {
		if previousEntry, err := s.entries.Get(int(previousIndex)); err == nil {
			resetHashFromEntry(previousEntry)
		}
	}

	if !s.cleanEnabled {
		if oldestEntry, err := s.entries.Peek(); err == nil {
			s.onEvict(oldestEntry, currentTimestamp, s.removeOldestEntry)
		}
	}

	for {
		if index, err := s.entries.Push(w); err == nil {
			s.hashmap[hashedKey] = uint64(index)
			return nil
		}
		if s.removeOldestEntry(NoSpace) != nil {
			return errors.New("entry is bigger than max shard size")
		}
	}
}

func (s *cacheShard) append(key string, hashedKey uint64, entry []byte) error {
	s.lock.Lock()
	wrappedEntry, err := s.getValidWrapEntry(key, hashedKey)

	if errors.Is(err, ErrEntryNotFound) {
		err = s.addNewWithoutLock(key, hashedKey, entry)
		s.lock.Unlock()
		return err
	}
	if err != nil {
		s.lock.Unlock()
		return err
	}

	currentTimestamp := uint64(s.clock.Epoch())

	w := appendToWrappedEntry(currentTimestamp, wrappedEntry, entry, &s.entryBuffer)

	err = s.setWrappedEntryWithoutLock(currentTimestamp, w, hashedKey)
	s.lock.Unlock()

	return err
}

func (s *cacheShard) del(hashedKey uint64) error {
	// 先加读锁检查是否存在 如果不存在直接返回
	s.lock.RLock()
	{
		itemIndex := s.hashmap[hashedKey]

		if itemIndex == 0 {
			s.lock.RUnlock()
			s.delmiss()
			return ErrEntryNotFound
		}

		if err := s.entries.CheckGet(int(itemIndex)); err != nil {
			s.lock.RUnlock()
			s.delmiss()
			return err
		}
	}
	s.lock.RUnlock()

	//加上写锁 真的删除：将其从 map 中删除、清空其对应 entry 的 hash 值
	s.lock.Lock()
	{
		itemIndex := s.hashmap[hashedKey]

		if itemIndex == 0 {
			s.lock.Unlock()
			s.delmiss()
			return ErrEntryNotFound
		}

		wrappedEntry, err := s.entries.Get(int(itemIndex))
		if err != nil {
			s.lock.Unlock()
			s.delmiss()
			return err
		}

		delete(s.hashmap, hashedKey)
		s.onRemove(wrappedEntry, Deleted)
		if s.statsEnabled {
			delete(s.hashmapStats, hashedKey)
		}
		resetHashFromEntry(wrappedEntry)
	}
	s.lock.Unlock()

	s.delhit()
	return nil
}

func (s *cacheShard) onEvict(oldestEntry []byte, currentTimestamp uint64, evict func(reason RemoveReason) error) bool {
	if s.isExpired(oldestEntry, currentTimestamp) {
		evict(Expired)
		return true
	}
	return false
}

func (s *cacheShard) isExpired(oldestEntry []byte, currentTimestamp uint64) bool {
	oldestTimestamp := readTimestampFromEntry(oldestEntry)
	if currentTimestamp <= oldestTimestamp { // if currentTimestamp < oldestTimestamp, the result will out of uint64 limits;
		return false
	}
	return currentTimestamp-oldestTimestamp > s.lifeWindow
}

func (s *cacheShard) cleanUp(currentTimestamp uint64) {
	s.lock.Lock()
	for {
		if oldestEntry, err := s.entries.Peek(); err != nil {
			break
		} else if evicted := s.onEvict(oldestEntry, currentTimestamp, s.removeOldestEntry); !evicted {
			break
		}
	}
	s.lock.Unlock()
}

func (s *cacheShard) getEntry(hashedKey uint64) ([]byte, error) {
	s.lock.RLock()

	entry, err := s.getWrappedEntry(hashedKey)
	// copy entry
	newEntry := make([]byte, len(entry))
	copy(newEntry, entry)

	s.lock.RUnlock()

	return newEntry, err
}

func (s *cacheShard) copyHashedKeys() (keys []uint64, next int) {
	s.lock.RLock()
	keys = make([]uint64, len(s.hashmap))

	for key := range s.hashmap {
		keys[next] = key
		next++
	}

	s.lock.RUnlock()
	return keys, next
}

// 删除最久的条目
func (s *cacheShard) removeOldestEntry(reason RemoveReason) (err error) {
	var oldest []byte
	if oldest, err = s.entries.Pop(); err == nil {
		hash := readHashFromEntry(oldest)
		if hash == 0 {
			// entry has been explicitly deleted with resetHashFromEntry, ignore
			return nil
		}
		delete(s.hashmap, hash)
		s.onRemove(oldest, reason)
		if s.statsEnabled {
			delete(s.hashmapStats, hash)
		}
		return nil
	}
	return err
}

func (s *cacheShard) reset(config Config) {
	s.lock.Lock()
	s.hashmap = make(map[uint64]uint64, config.initialShardSize())
	s.entryBuffer = make([]byte, config.MaxEntrySize+headersSizeInBytes)
	s.entries.Reset()
	s.lock.Unlock()
}

func (s *cacheShard) resetStats() {
	s.lock.Lock()
	s.stats = Stats{}
	s.lock.Unlock()
}

func (s *cacheShard) len() int {
	s.lock.RLock()
	res := len(s.hashmap)
	s.lock.RUnlock()
	return res
}

func (s *cacheShard) capacity() int {
	s.lock.RLock()
	res := s.entries.Capacity()
	s.lock.RUnlock()
	return res
}

func (s *cacheShard) getStats() Stats {
	var stats = Stats{
		Hits:       atomic.LoadInt64(&s.stats.Hits),
		Misses:     atomic.LoadInt64(&s.stats.Misses),
		DelHits:    atomic.LoadInt64(&s.stats.DelHits),
		DelMisses:  atomic.LoadInt64(&s.stats.DelMisses),
		Collisions: atomic.LoadInt64(&s.stats.Collisions),
	}
	return stats
}

func (s *cacheShard) getKeyMetadataWithLock(key uint64) Metadata {
	s.lock.RLock()
	c := s.hashmapStats[key]
	s.lock.RUnlock()
	return Metadata{
		RequestCount: c,
	}
}

func (s *cacheShard) getKeyMetadata(key uint64) Metadata {
	return Metadata{
		RequestCount: s.hashmapStats[key],
	}
}

func (s *cacheShard) hit(key uint64) {
	atomic.AddInt64(&s.stats.Hits, 1)
	if s.statsEnabled {
		s.lock.Lock()
		s.hashmapStats[key]++
		s.lock.Unlock()
	}
}

func (s *cacheShard) hitWithoutLock(key uint64) {
	atomic.AddInt64(&s.stats.Hits, 1)
	if s.statsEnabled {
		s.hashmapStats[key]++
	}
}

func (s *cacheShard) miss() {
	atomic.AddInt64(&s.stats.Misses, 1)
}

func (s *cacheShard) delhit() {
	atomic.AddInt64(&s.stats.DelHits, 1)
}

func (s *cacheShard) delmiss() {
	atomic.AddInt64(&s.stats.DelMisses, 1)
}

func (s *cacheShard) collision() {
	atomic.AddInt64(&s.stats.Collisions, 1)
}

func initNewShard(config Config, callback onRemoveCallback, clock clock) *cacheShard {
	bytesQueueInitialCapacity := config.initialShardSize() * config.MaxEntrySize
	maximumShardSizeInBytes := config.maximumShardSizeInBytes()
	if maximumShardSizeInBytes > 0 && bytesQueueInitialCapacity > maximumShardSizeInBytes {
		bytesQueueInitialCapacity = maximumShardSizeInBytes
	}
	return &cacheShard{
		hashmap:      make(map[uint64]uint64, config.initialShardSize()),
		hashmapStats: make(map[uint64]uint32, config.initialShardSize()),
		entries:      *queue.NewBytesQueue(bytesQueueInitialCapacity, maximumShardSizeInBytes, config.Verbose),
		entryBuffer:  make([]byte, config.MaxEntrySize+headersSizeInBytes),
		onRemove:     callback,

		isVerbose:    config.Verbose,
		logger:       newLogger(config.Logger),
		clock:        clock,
		lifeWindow:   uint64(config.LifeWindow.Seconds()),
		statsEnabled: config.StatsEnabled,
		cleanEnabled: config.CleanWindow > 0,
	}
}
