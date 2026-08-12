package base58

import (
	"crypto/rand"
	mrand "math/rand"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEncodeCached32_MatchesEncode32(t *testing.T) {
	for range 5000 {
		var src [32]byte
		rand.Read(src[:])
		want := Encode32(&src)
		assert.Equal(t, want, EncodeCached32(&src))
		// Second call: served from cache, must still match.
		assert.Equal(t, want, EncodeCached32(&src))
	}
}

func TestEncodeCached32_Concurrent(t *testing.T) {
	// Hammer a small key set from many goroutines; every result must be
	// exactly the canonical encoding (guards against torn entries).
	keys := make([][32]byte, 64)
	want := make([]string, 64)
	for i := range keys {
		rand.Read(keys[i][:])
		want[i] = Encode32(&keys[i])
	}
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(1)
		go func(seed int64) {
			defer wg.Done()
			rng := mrand.New(mrand.NewSource(seed))
			for range 20000 {
				i := rng.Intn(len(keys))
				if got := EncodeCached32(&keys[i]); got != want[i] {
					t.Errorf("mismatch for key %d", i)
					return
				}
			}
		}(int64(g))
	}
	wg.Wait()
}

func BenchmarkBase58_EncodeCached32_Hit(b *testing.B) {
	src := &benchSrc32
	EncodeCached32(src) // warm
	b.SetBytes(32)
	for b.Loop() {
		EncodeCached32(src)
	}
}

// BenchmarkBase58_EncodeCached32_Zipf approximates a block-render key mix:
// a small hot set (programs, sysvars, mints) plus a long tail.
func BenchmarkBase58_EncodeCached32_Zipf(b *testing.B) {
	keys := make([][32]byte, 4096)
	for i := range keys {
		rand.Read(keys[i][:])
	}
	rng := mrand.New(mrand.NewSource(42))
	zipf := mrand.NewZipf(rng, 1.2, 8, uint64(len(keys)-1))
	order := make([]int, 1<<16)
	for i := range order {
		order[i] = int(zipf.Uint64())
	}
	b.SetBytes(32)
	i := 0
	for b.Loop() {
		EncodeCached32(&keys[order[i&(1<<16-1)]])
		i++
	}
}
