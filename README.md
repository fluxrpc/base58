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

`Decode32`/`Decode64` are allocation-free. The `Append*` functions are also
allocation-free through 1 KiB when `dst` has enough capacity; larger inputs
use bounded scratch space. `Encode`/`Encode32`/`Encode64` allocate the returned
string. For RPC and JSON hot paths, reuse a buffer with `AppendEncode*`.

```go
buf := make([]byte, 0, base58.EncodedMaxLen32)
for _, pk := range pubkeys {
    buf = base58.AppendEncode32(buf[:0], &pk) // 0 allocs/op
    sink(buf)
}
```

## Performance

Intel i7-9700K, AVX2, one pinned WSL2 CPU, Go 1.24.0. Small-input results are
medians over 4,096 deterministic values; large-input results are one pass.

### Current throughput

| Payload | `AppendEncode` | `AppendDecode` | Encode / decode scratch |
|---:|---:|---:|---:|
| 16 B | ~44.7 ns | ~39.2 ns | 0 B / 0 B |
| 32 B | ~43.8 ns | ~47.7 ns | 0 B / 0 B |
| 48 B | ~136 ns | ~93.2 ns | 0 B / 0 B |
| 64 B | ~93.2 ns | ~75.0 ns | 0 B / 0 B |
| 128 B | ~444 ns | ~350 ns | 0 B / 0 B |
| 1000 B | ~12.3 µs | ~11.7 µs | 0 B / 0 B |
| **1 MiB** | **1.60 s** | **0.550 s** | 36.0 MB / 55.7 MB |
| **5 MiB** | **20.5 s** | **7.39 s** | 235 MB / 307 MB |
| **10 MiB** | **47.9 s** | **26.0 s** | 485 MB / 678 MB |

### v1.1.0 speedups

| Operation | Before | v1.1.0 | Speedup |
|---|---:|---:|---:|
| Encode 16 B | ~72 ns | ~37 ns | **1.9x** |
| Encode 48 B | ~197 ns | ~130 ns | **1.5x** |
| Encode 65 B | ~318 ns | ~184 ns | **1.7x** |
| Encode 100 B | ~529 ns | ~375 ns | **1.4x** |
| Encode 128 B | ~820 ns | ~425 ns | **1.9x** |
| Encode 1000 B | ~34.9 µs, 2 allocs | ~10.5–12.3 µs, 0 allocs | **2.8–3.3x** |
| Encode 1 MiB | 11.8 s | 1.60 s | **7.4x** |
| Decode 1 MiB | 14.1 s | 0.550 s | **25.6x** |

The previous quadratic path made 5 MiB and 10 MiB baselines impractical, so
only current results are reported for those sizes.

### Versus base58-turbo v0.3.0

| Payload | Encode winner | Decode winner |
|---:|---:|---:|
| 16 B | Go **1.13x** | Go **1.23x** |
| 32 B | Turbo **1.13x** | Go **1.16x** |
| 48 B | Turbo **1.84x** | Turbo **1.82x** |
| 64 B | Turbo **1.51x** | Turbo **1.20x** |
| 128 B | parity | Turbo **1.60x** |
| 1000 B | Go **1.15x** | Turbo **1.60x** |
| 1 MiB | Go **6.1x** | Go **10.7x** |

Small sizes compare zero-allocation output-buffer APIs on the same corpus.
The 1 MiB row compares Turbo's allocating unbounded API on the same payload;
its 5 MiB and 10 MiB cases were not run because that path remains quadratic.
Treat differences below 10% as parity.

### Solana hot paths

| Operation | Time | Allocations |
|---|---:|---:|
| `AppendEncode32` | ~32.9 ns | 0 |
| `Decode32` | ~30.2 ns | 0 |
| `AppendEncode64` | ~73.8 ns | 0 |
| `Decode64` | ~59.9 ns | 0 |
| `EncodeCached32` hit | ~5.40 ns | 0 |

### Reproduce

```sh
# 16 B–1 KiB corpus
GOMAXPROCS=1 go test -run '^$' \
  -bench 'BenchmarkBase58_Append(Encode|Decode)Corpus' \
  -benchmem -benchtime=1s -count=5

# 1, 5, and 10 MiB account payloads
GOMAXPROCS=1 go test -run '^$' \
  -bench 'BenchmarkBase58_Append(Encode|Decode)Large' \
  -benchmem -benchtime=1x -count=1
```

CI rejects small-payload slowdowns over 20% or new allocations, and separately
runs every 1/5/10 MiB benchmark.

Correctness is cross-validated against Bitcoin Core / bs58 / fd_base58 /
five8 test vectors, randomized round-trips against the independent
variable-length implementation, and fuzzing (`go test -fuzz`). The AVX2
kernels are gated by runtime CPUID detection; scalar assembly (amd64,
arm64) and pure Go fallbacks are exercised by `TestScalarFallback_Matches`
and cross-architecture builds.

## License and attribution

This package contains an Apache-2.0-licensed Go/assembly port derived from
Firedancer's base58 implementation and the corresponding solana-go port. See
`LICENSE` and `NOTICE` for the retained upstream terms and attribution.
The variable-width performance work was informed by the
`hacer-bark/base58-turbo` project.

Original modifications and additions authored by AlphaBatem Labs are also
available under the scoped MIT terms in `LICENSE-MIT`. That MIT grant applies
only to AlphaBatem Labs' work and does not relicense upstream material.
