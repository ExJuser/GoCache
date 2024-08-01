package GoCache

import (
	"context"
	"crypto/rand"
	"fmt"
	"github.com/stretchr/testify/assert"
	random "math/rand/v2"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWriteAndGetOnCache(t *testing.T) {
	t.Parallel()

	cache, _ := New(context.Background(), DefaultConfig(5*time.Second))
	value := []byte("value")

	_ = cache.Set("key", value)
	cachedValue, err := cache.Get("key")

	assert.NoError(t, err)
	assert.Equal(t, value, cachedValue)
}

func TestAppendAndGetOnCache(t *testing.T) {
	t.Parallel()

	cache, _ := New(context.Background(), DefaultConfig(5*time.Second))
	key := "key"
	value1 := make([]byte, 50)
	_, _ = rand.Read(value1)
	value2 := make([]byte, 50)
	_, _ = rand.Read(value2)
	value3 := make([]byte, 50)
	_, _ = rand.Read(value3)

	_, err := cache.Get(key)

	assert.Equal(t, ErrEntryNotFound, err)

	_ = cache.Append(key, value1)
	cachedValue, err := cache.Get(key)

	assert.NoError(t, err)
	assert.Equal(t, value1, cachedValue)

	_ = cache.Append(key, value2)
	cachedValue, err = cache.Get(key)

	assert.NoError(t, err)
	expectedValue := value1
	expectedValue = append(expectedValue, value2...)
	assert.Equal(t, expectedValue, cachedValue)

	_ = cache.Append(key, value3)
	cachedValue, err = cache.Get(key)

	assert.NoError(t, err)
	expectedValue = value1
	expectedValue = append(expectedValue, value2...)
	expectedValue = append(expectedValue, value3...)
	assert.Equal(t, expectedValue, cachedValue)
}

func TestAppendRandomly(t *testing.T) {
	t.Parallel()

	c := Config{
		Shards:             1,
		LifeWindow:         5 * time.Second,
		CleanWindow:        1 * time.Second,
		MaxEntriesInWindow: 1000 * 10 * 60,
		MaxEntrySize:       500,
		StatsEnabled:       true,
		Verbose:            true,
		Hasher:             newDefaultHasher(),
		HardMaxCacheSize:   1,
		Logger:             DefaultLogger(),
	}
	cache, err := New(context.Background(), c)
	assert.NoError(t, err)

	nKeys := 5
	nAppendsPerKey := 2000
	nWorker := 10
	var keys []string
	for i := 0; i < nKeys; i++ {
		for j := 0; j < nAppendsPerKey; j++ {
			keys = append(keys, fmt.Sprintf("key%d", i))
		}
	}
	random.Shuffle(len(keys), func(i, j int) {
		keys[i], keys[j] = keys[j], keys[i]
	})

	jobs := make(chan string, len(keys))
	for _, key := range keys {
		jobs <- key
	}
	close(jobs)

	var wg sync.WaitGroup
	for i := 0; i < nWorker; i++ {
		wg.Add(1)
		go func() {
			for {
				key, ok := <-jobs
				if !ok {
					break
				}
				_ = cache.Append(key, []byte(key))
			}
			wg.Done()
		}()
	}
	wg.Wait()

	assert.Equal(t, nKeys, cache.Len())
	for i := 0; i < nKeys; i++ {
		key := fmt.Sprintf("key%d", i)
		expectedValue := []byte(strings.Repeat(key, nAppendsPerKey))
		cachedValue, err := cache.Get(key)
		assert.NoError(t, err)
		assert.Equal(t, expectedValue, cachedValue)
	}
}
