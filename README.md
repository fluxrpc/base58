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

Same machine (Go 1.26, i7-9700K,
AVX2, single core), same inputs, both sides measured through the generic
`Encode([]byte)`/`Decode(string)` entry points; averages of 5 runs.

| Operation | solana-foundation/solana-go | cloakd/base58   | speedup |
|---|---|-----------------|---|
| Encode 32B (pubkey) | 364 ns, 2 allocs | 68 ns, 1 alloc  | **5.3x** |
| Decode 32B | 142 ns, 2 allocs | 60 ns, 1 alloc  | **2.4x** |
| Encode 64B (signature) | 1,116 ns, 2 allocs | 153 ns, 1 alloc | **7.3x** |
| Decode 64B | 296 ns, 3 allocs | 128 ns, 1 alloc | **2.3x** |
| Encode 16B | 134 ns | 100 ns          | 1.3x |
| Decode 16B | 85 ns | 46 ns           | 1.8x |
| Encode 100B | 2,378 ns, 5 allocs | 541 ns, 1 alloc | **4.4x** |
| Decode 100B | 385 ns, 3 allocs | 240 ns, 1 alloc | **1.6x** |
| Encode 1000B | 187,427 ns | 34,600 ns       | **5.4x** |
| Decode 1000B | 11,443 ns | 11,920 ns       | 1.0x |

The typed entry points are faster still (no dispatch, no output-slice copy,
zero allocations for decode/append):

| Operation | ns/op | allocs/op |
|---|---|---|
| Encode32 (pubkey) | ~63 | 1 |
| EncodeCached32 (repeated key) | ~5.7 | 0 |
| EncodeCached32 (Zipf key mix) | ~15 | ~0 |
| AppendEncode32 | ~41 | 0 |
| Encode32Batch (per key) | ~48 | 2 per batch |
| Decode32 | ~34 | 0 |
| Encode64 (signature) | ~127 | 1 |
| AppendEncode64 | ~101 | 0 |
| Encode64Batch (per key) | ~100 | 2 per batch |
| Decode64 | ~68 | 0 |

The comparison benchmark is not checked in (keeping the module
dependency-free); to reproduce, add `solana-go/base58` as a test dependency
and benchmark `base58.Encode`/`Decode` against `Encode`/`Decode` on the
same inputs.

Correctness is cross-validated against Bitcoin Core / bs58 / fd_base58 /
five8 test vectors, randomized round-trips against the independent
variable-length implementation, and fuzzing (`go test -fuzz`). The AVX2
kernels are gated by runtime CPUID detection; scalar assembly (amd64,
arm64) and pure Go fallbacks are exercised by `TestScalarFallback_Matches`
and cross-architecture builds.
