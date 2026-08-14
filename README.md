# base58

Fast base58 encoding & decoding for Go, tuned for Solana-shaped workloads:
32-byte public keys and 64-byte signatures use Firedancer-style
matrix-multiply fast paths (AVX2 assembly on amd64 with scalar-assembly and
pure-Go fallbacks), and all other lengths use a limb-based codec that is
~10-30x faster than the classic byte-at-a-time long division.

Created & used heavily by [FluxRPC.com](https://FluxRPC.com) - The most performant Solana RPC Provider.

## API

```go
// Variable length (dispatches to the fixed-size fast paths for 32/64).
func Encode(buf []byte) string
func Decode(encoded string) ([]byte, error)

// Zero-allocation append variants of Encode/Decode (reuse dst across calls).
func AppendEncode(dst []byte, src []byte) []byte
func AppendDecode(dst []byte, encoded string) ([]byte, error)

// Fixed-size fast paths.
func Encode32(src *[32]byte) string
func Encode64(src *[64]byte) string
func Decode32(encoded string, dst *[32]byte) error
func Decode64(encoded string, dst *[64]byte) error

// Zero-allocation append variants.
func AppendEncode32(dst []byte, src *[32]byte) []byte
func AppendEncode64(dst []byte, src *[64]byte) []byte

// Batch encoders: four keys at a time with interleaved carry chains; all
// returned strings share one backing buffer (2 allocs per batch, ~35%
// less CPU per key than Encode32/Encode64 in a loop).
func Encode32Batch(srcs [][32]byte) []string
func Encode64Batch(srcs [][64]byte) []string

// Interning cache for repeated keys (program IDs, sysvars, mints, hot
// accounts): hits return the previously built string in ~6 ns with zero
// allocations. Opt-in; see the doc comment for the collision caveat.
func EncodeCached32(src *[32]byte) string
```

`Decode32`/`Decode64` and the `Append*` functions perform **zero heap
allocations**. `Encode`/`Encode32`/`Encode64` allocate exactly once (the
returned string). In hot paths that write into a larger buffer (RPC
rendering, JSON assembly), prefer `AppendEncode*` with a reused buffer —
encoding then allocates nothing at all.

```go
buf := make([]byte, 0, base58.EncodedMaxLen32)
for _, pk := range pubkeys {
    buf = base58.AppendEncode32(buf[:0], &pk) // 0 allocs/op
    sink(buf)
}
```

## Performance

### Comparisons

Same machine (Go 1.26.1, Intel i7-9700K under WSL2, AVX2, one pinned CPU,
`GOMAXPROCS=1`), same 4,096 deterministic inputs per size, and both sides
measured in the same benchmark binary through the generic
`Encode([]byte)`/`Decode(string)` entry points. Results are medians of five
1-second runs, comparing `fluxrpc/base58` main at `1c233aac` with the current
`solana-foundation/solana-go` main at `2716b505`.

| Operation | solana-foundation/solana-go | fluxrpc/base58 | Speedup |
|---|---:|---:|---:|
| Encode 16B | 311 ns, 1 alloc | 91.6 ns, 1 alloc | **3.40x** |
| Decode 16B | 277 ns, 1 alloc | 63.2 ns, 1 alloc | **4.38x** |
| Encode 32B (pubkey) | 125 ns, 1 alloc | 65.2 ns, 1 alloc | **1.92x** |
| Decode 32B | 111 ns, 1 alloc | 63.8 ns, 1 alloc | **1.75x** |
| Encode 64B (signature) | 474 ns, 1 alloc | 121 ns, 1 alloc | **3.91x** |
| Decode 64B | 392 ns, 1 alloc | 112 ns, 1 alloc | **3.50x** |
| Encode 100B | 8,738 ns, 3 allocs | 578 ns, 1 alloc | **15.13x** |
| Decode 100B | 7,960 ns, 2 allocs | 312 ns, 1 alloc | **25.49x** |
| Encode 1000B | 845,012 ns, 3 allocs | 35,911 ns, 2 allocs | **23.53x** |
| Decode 1000B | 718,747 ns, 2 allocs | 10,640 ns, 2 allocs | **67.55x** |

The typed entry points are faster still (no dispatch, no output-slice copy,
zero allocations for decode/append):

| Operation | ns/op | allocs/op |
|---|---|---|
| Encode32 (pubkey) | ~57.2 | 1 |
| EncodeCached32 (repeated key) | ~5.40 | 0 |
| EncodeCached32 (Zipf key mix) | ~14.7 | 0 |
| AppendEncode32 | ~32.9 | 0 |
| Encode32Batch (per key) | ~50.5 | 2 per batch |
| Decode32 | ~30.2 | 0 |
| Encode64 (signature) | ~107.6 | 1 |
| AppendEncode64 | ~73.8 | 0 |
| Encode64Batch (per key) | ~100.2 | 2 per batch |
| Decode64 | ~59.9 | 0 |

The comparison benchmark is not checked in (keeping the module
dependency-free). To reproduce it, benchmark against the current
`solana-foundation/solana-go/base58` checkout in a temporary module, using
the same input corpus for both implementations.

Correctness is cross-validated against Bitcoin Core / bs58 / fd_base58 /
five8 test vectors, randomized round-trips against the independent
variable-length implementation, and fuzzing (`go test -fuzz`). The AVX2
kernels are gated by runtime CPUID detection; scalar assembly (amd64,
arm64) and pure Go fallbacks are exercised by `TestScalarFallback_Matches`
and cross-architecture builds.
