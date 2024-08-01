package GoCache

import (
	"context"
	"github.com/stretchr/testify/assert"
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
