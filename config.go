package GoCache

import "time"

type Config struct {
	Shards               int
	LifeWindow           time.Duration
	CleanWindow          time.Duration
	MaxEntriesInWindow   int
	MaxEntrySize         int
	StatsEnabled         bool
	Verbose              bool
	Hasher               Hasher
	HardMaxCacheSize     int
	OnRemove             func(key string, entry []byte)
	OnRemoveWithMetadata func(key string, entry []byte, keyMetadata Metadata)
	OnRemoveWithReason   func(key string, entry []byte, reason RemoveReason)
	onRemoveFilter       int
	Logger               Logger
}

func DefaultConfig(eviction time.Duration) Config {
	return Config{
		Shards:             1024,
		LifeWindow:         eviction,
		CleanWindow:        1 * time.Second,
		MaxEntriesInWindow: 1000 * 10 * 60,
		MaxEntrySize:       500,
		StatsEnabled:       false,
		Verbose:            true,
		Hasher:             newDefaultHasher(),
		HardMaxCacheSize:   0,
		Logger:             DefaultLogger(),
	}
}

func (c Config) initialShardSize() int {
	return max(c.MaxEntriesInWindow/c.Shards, minimumEntriesInShard)
}

func (c Config) maximumShardSizeInBytes() int {
	maxShardSize := 0
	if c.HardMaxCacheSize > 0 {
		maxShardSize = convertMBToBytes(c.HardMaxCacheSize) / c.Shards
	}
	return maxShardSize
}

func (c Config) OnRemoveFilterSet(reasons ...RemoveReason) Config {
	c.onRemoveFilter = 0
	for i := range reasons {
		c.onRemoveFilter |= 1 << uint(reasons[i])
	}
	return c
}

func convertMBToBytes(value int) int {
	return value * 1024 * 1024
}
