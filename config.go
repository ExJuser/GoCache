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
