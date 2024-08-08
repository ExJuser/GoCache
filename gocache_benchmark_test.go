package GoCache

import (
	"context"
	"math"
	"math/rand/v2"
	"strconv"
	"sync"
	"testing"
	"time"
)

const (
	SetTimes    = 1000000
	GetTimes    = 1000000
	RandomTimes = 1000000
	RandomBound = math.MaxInt
)

type MapWithRWMutex struct {
	sync.RWMutex
	mp map[string][]byte
}

func NewMapWithRWMutex() *MapWithRWMutex {
	return &MapWithRWMutex{
		mp: make(map[string][]byte),
	}
}

func (m *MapWithRWMutex) Set(key string, value []byte) {
	m.Lock()
	defer m.Unlock()
	m.mp[key] = value
}

func (m *MapWithRWMutex) Get(key string) (value []byte, ok bool) {
	m.RLock()
	defer m.RUnlock()
	value, ok = m.mp[key]
	return
}

func BenchmarkGoCache_Set(b *testing.B) {
	cache, _ := New(context.Background(), DefaultConfig(5*time.Second))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < SetTimes; j++ {
			go func(j int) {
				key := strconv.Itoa(j)
				value := []byte(key)
				_ = cache.Set(key, value)
			}(j)
		}
	}
}

func BenchmarkGoCache_Get(b *testing.B) {
	cache, _ := New(context.Background(), DefaultConfig(5*time.Second))
	for j := 0; j < SetTimes; j++ {
		go func(j int) {
			key := strconv.Itoa(j)
			value := []byte(key)
			_ = cache.Set(key, value)
		}(j)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < GetTimes; j++ {
			go func(j int) {
				key := strconv.Itoa(rand.IntN(RandomBound))
				_, _ = cache.Get(key)
			}(j)
		}
	}
}

func BenchmarkGoCache_Random(b *testing.B) {
	//是读还是写完全随机
	//读写的元素也完全随机
	cache, _ := New(context.Background(), DefaultConfig(5*time.Second))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < RandomTimes; j++ {
			go func(j int) {
				op := rand.IntN(2)
				if op == 0 { //写
					key := strconv.Itoa(rand.IntN(RandomBound))
					value := []byte(key)
					_ = cache.Set(key, value)
				} else { //读
					key := strconv.Itoa(rand.IntN(RandomBound))
					_, _ = cache.Get(key)
				}
			}(j)
		}
	}
}

func BenchmarkMapWithRWMutex_Set(b *testing.B) {
	mp := NewMapWithRWMutex()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < SetTimes; j++ {
			go func(j int) {
				key := strconv.Itoa(j)
				value := []byte(key)
				mp.Set(key, value)
			}(j)
		}
	}
}

func BenchmarkMapWithRWMutex_Get(b *testing.B) {
	mp := NewMapWithRWMutex()
	for j := 0; j < SetTimes; j++ {
		go func(j int) {
			key := strconv.Itoa(j)
			value := []byte(key)
			mp.Set(key, value)
		}(j)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < GetTimes; j++ {
			go func(j int) {
				key := strconv.Itoa(rand.IntN(RandomBound))
				mp.Get(key)
			}(j)
		}
	}
}

func BenchmarkMapWithRWMutex_Random(b *testing.B) {
	mp := NewMapWithRWMutex()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < RandomTimes; j++ {
			go func() {
				op := rand.IntN(2)
				if op == 0 { //写
					key := strconv.Itoa(rand.IntN(RandomBound))
					value := []byte(key)
					mp.Set(key, value)
				} else { //读
					key := strconv.Itoa(rand.IntN(RandomBound))
					mp.Get(key)
				}
			}()
		}
	}
}
