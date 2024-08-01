package GoCache

type Stats struct {
	Hits       int64 `json:"hits"`
	Misses     int64 `json:"misses"`
	DelHits    int64 `json:"delete_hits"`
	DelMisses  int64 `json:"delete_misses"`
	Collisions int64 `json:"collisions"`
}
