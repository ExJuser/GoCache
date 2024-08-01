package GoCache

const (
	minimumEntriesInShard = 10
)

type Response struct {
	EntryStatus RemoveReason
}

type RemoveReason uint32

const (
	Expired = RemoveReason(1)
	NoSpace = RemoveReason(2)
	Deleted = RemoveReason(3)
)
