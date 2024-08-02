package GoCache

// Stats 记录缓存的命中、Miss等统计数值
type Stats struct {
	// Hits 缓存命中次数
	Hits int64 `json:"hits"`
	// Misses 缓存Miss次数
	Misses int64 `json:"misses"`
	// DelHits 成功删除的次数
	DelHits int64 `json:"delete_hits"`
	// DelMisses 删除Miss的次数
	DelMisses int64 `json:"delete_misses"`
	// Collisions 发生键碰撞冲突的次数
	Collisions int64 `json:"collisions"`
}
