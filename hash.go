package GoCache

// Hasher 负责生成Key的无符号64位哈希值：尽可能地减少冲突（即为不同的Key生成不同的哈希值）
type Hasher interface {
	Sum64(string) uint64
}
