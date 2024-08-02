package GoCache

import "time"

// Config GoCache 的一些配置
type Config struct {
	// Shards 分片数量 必须是 2 的次方
	Shards int

	// LifeWindow 一个键值对的过期时间
	LifeWindow time.Duration

	// CleanWindow 清理过期键值对的间隔时间
	CleanWindow time.Duration

	// MaxEntriesInWindow 最大键值对数量；仅用于计算缓存分片的初始大小
	MaxEntriesInWindow int

	// MaxEntrySize 键值对的最大大小；仅用于计算缓存分片的初始大小
	MaxEntrySize int

	// StatsEnabled 是否记录缓存命中、Miss的一些统计数据
	StatsEnabled bool

	// Verbose 是否打印日志
	Verbose bool

	// Hasher 将字符串映射为无符号64位整数
	Hasher Hasher

	// HardMaxCacheSize 实际存储数据的 BytesQueue的最大内存限制；防止GoCache消耗完机器的所有可用内存；
	// 默认值为0 代表不对内存大小进行限制; 最旧的键值对将被新的键值对覆盖
	HardMaxCacheSize int

	// OnRemove 键值对被删除时的回调函数：过期、空间不足被新的键值对覆盖、主动删除等
	// 如果 OnRemoveWithMetadata 不为nil 则忽略
	OnRemove func(key string, entry []byte)

	// OnRemoveWithMetadata 相比于 OnRemove 提供更详细的信息
	OnRemoveWithMetadata func(key string, entry []byte, keyMetadata Metadata)

	// OnRemoveWithReason 相比于 OnRemove 提供原因
	// 如果 OnRemove 不为nil 则忽略
	OnRemoveWithReason func(key string, entry []byte, reason RemoveReason)

	onRemoveFilter int

	// Logger 日志记录接口 与 `Verbose` 结合使用 默认为 `DefaultLogger()`
	Logger Logger
}

// DefaultConfig 默认配置
func DefaultConfig(eviction time.Duration) Config {
	return Config{
		Shards:             1024,        // 默认1024个分片
		LifeWindow:         eviction,    //手动传入过期时间
		CleanWindow:        time.Second, //默认一秒钟清理一次
		MaxEntriesInWindow: 1000 * 10 * 60,
		MaxEntrySize:       500,
		StatsEnabled:       false, //默认不开启数据记录
		Verbose:            true,  //默认开启日志打印
		Hasher:             newDefaultHasher(),
		HardMaxCacheSize:   0, //默认不开启内存限制
		Logger:             DefaultLogger(),
	}
}

func (c Config) initialShardSize() int {
	//整个GoCache的最大键值对数量/分片总数=一个分片的平均最大键值对数量
	//再与最小的单分片键值对数量取最大值确认初始的分片大小
	return max(c.MaxEntriesInWindow/c.Shards, minimumEntriesInShard)
}

func (c Config) maximumShardSizeInBytes() int {
	maxShardSize := 0
	//如果开启了内存限制
	if c.HardMaxCacheSize > 0 {
		//单个分片的平均内存限制
		maxShardSize = convertMBToBytes(c.HardMaxCacheSize) / c.Shards
	}
	//不开启内存限制的情况下默认为0
	return maxShardSize
}

func (c Config) OnRemoveFilterSet(reasons ...RemoveReason) Config {
	c.onRemoveFilter = 0
	for i := range reasons {
		c.onRemoveFilter |= 1 << uint(reasons[i])
	}
	return c
}

// 将MB转换为B
func convertMBToBytes(value int) int {
	return value * 1024 * 1024
}
