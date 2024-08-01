package GoCache

const (
	minimumEntriesInShard = 10 // Minimum number of entries in single shard
)

type Response struct {
	EntryStatus RemoveReason
}

// RemoveReason is a value used to signal to the user why a particular key was removed in the OnRemove callback.
type RemoveReason uint32

const (
	// Expired means the key is past its LifeWindow.
	Expired = RemoveReason(1)
	// NoSpace means the key is the oldest and the cache size was at its maximum when Set was called, or the
	// entry exceeded the maximum shard size.
	NoSpace = RemoveReason(2)
	// Deleted means Delete was called and this key was removed as a result.
	Deleted = RemoveReason(3)
)
