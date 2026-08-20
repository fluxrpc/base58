# base58

High-performance Base58 encoding and decoding for Go, from short identifiers
and cryptographic keys to megabyte payloads. Common 32- and 64-byte inputs use
AVX2 on amd64, with scalar assembly and pure-Go fallbacks. Variable payloads
use size-tiered algorithms that avoid classic byte-at-a-time long division.

Created and used heavily by [FluxRPC](https://FluxRPC.com).

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

// Interning cache for repeated keys and identifiers: hits return the
// previously built string in ~6 ns with zero allocations. Opt-in; see the
// doc comment for the collision caveat.
func EncodeCached32(src *[32]byte) string
```

`Decode32`/`Decode64` and `AppendEncode32`/`AppendEncode64` are allocation-free
when the destination has enough capacity. Generic `Append*` calls reuse the
destination but may allocate working memory for large inputs. `Encode*`
allocates the returned string.

```go
buf := make([]byte, 0, base58.EncodedMaxLen32)
for _, pk := range pubkeys {
    buf = base58.AppendEncode32(buf[:0], &pk) // 0 allocs/op
    sink(buf)
}
```

## Benchmarks

### Fixed sizes

Same-machine comparison against `solana-go` commit `49f4779`. Both
implementations ran in the same binary with identical inputs on a pinned
Intel i7-9700K core using AVX2, WSL2, and Go 1.24.0.

| Operation | `solana-go/base58` | This package | Improvement |
|---|---:|---:|---:|
| `Encode32` | 62.41 ns | **56.90 ns** | **8.8% faster** |
| `AppendEncode32` | 43.24 ns | **30.81 ns** | **28.8% faster** |
| `Decode32` | 23.33 ns | **19.55 ns** | **16.2% faster** |
| `Encode64` | 95.97 ns | **93.35 ns** | **2.7% faster** |
| `AppendEncode64` | 71.96 ns | **66.28 ns** | **7.9% faster** |
| `Decode64` | 38.15 ns | **30.48 ns** | **20.1% faster** |

Median of 13 alternating-order runs at 100 ms each. Encode allocates once;
AppendEncode and Decode allocate nothing.

### Variable sizes

Public allocating `Encode` / `Decode` APIs on identical deterministic input.

| Input | `solana-go/base58` Encode / Decode | This package Encode / Decode | Speedup Encode / Decode |
|---:|---:|---:|---:|
| 128 B | 14.3 µs / 13.2 µs | **459 ns / 339 ns** | **31× / 39×** |
| 256 B | 54.0 µs / 48.7 µs | **1.21 µs / 924 ns** | **45× / 53×** |
| 512 B | 233 µs / 201 µs | **3.46 µs / 3.17 µs** | **67× / 63×** |
| 1 KiB | 825 µs / 746 µs | **11.6 µs / 10.5 µs** | **71× / 71×** |
| 4 KiB | 14.4 ms / 11.7 ms | **151 µs / 147 µs** | **96× / 80×** |
| 16 KiB | 220 ms / 188 ms | **2.00 ms / 1.15 ms** | **110× / 164×** |
| 64 KiB | 3.40 s / 3.02 s | **16.1 ms / 7.36 ms** | **211× / 410×** |
| 256 KiB | — | **134 ms / 63.8 ms** | — |
| 1 MiB | — | **1.06 s / 585 ms** | — |

Medians of alternating-order runs: 13 × 100 ms through 1 KiB, then three
single-operation runs. The `solana-go` baseline is omitted above 64 KiB because
its quadratic fallback already took 54 s to encode 256 KiB.

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
Original modifications and additions authored by AlphaBatem Labs are also
available under the scoped MIT terms in `LICENSE-MIT`. That MIT grant applies
only to AlphaBatem Labs' work and does not relicense upstream material.
