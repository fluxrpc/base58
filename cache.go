package base58

import (
	"encoding/binary"
	"sync/atomic"
)

// enc32CacheBits sizes the interning cache: 2^14 slots ≈ 16K hot keys,
// worst-case ~2 MB retained (entry + backing string per slot).
const enc32CacheBits = 14

type enc32Entry struct {
	key [32]byte
	val string
}

// enc32Cache is a fixed-size, lock-free, direct-mapped intern table.
// Entries are immutable once published; a slot swap on collision simply
// replaces which key stays hot. Safe for concurrent use: readers load the
// entry pointer atomically and verify the full 32-byte key before trusting
// the value.
var enc32Cache [1 << enc32CacheBits]atomic.Pointer[enc32Entry]

// EncodeCached32 is Encode32 with interning for repeated keys. Solana
// workloads re-encode the same 32-byte keys constantly (program IDs,
// sysvars, mints, hot accounts); a hit returns the previously built string
// in a few nanoseconds with zero allocations. Misses cost one Encode32
// plus one cache-entry allocation.
//
// The cache is direct-mapped on the key's first 8 bytes (uniform for
// ed25519 public keys). Do not use it for attacker-chosen keys where
// deliberate slot collisions could keep the hit rate at zero — plain
// Encode32 is the safe default there.
func EncodeCached32(src *[32]byte) string {
	idx := (binary.LittleEndian.Uint64(src[0:8]) * 0x9E3779B97F4A7C15) >> (64 - enc32CacheBits)
	slot := &enc32Cache[idx]
	if e := slot.Load(); e != nil && e.key == *src {
		return e.val
	}
	e := &enc32Entry{key: *src, val: Encode32(src)}
	slot.Store(e)
	return e.val
}
