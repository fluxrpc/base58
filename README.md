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

The new corpus benchmarks use the same 4,096 deterministic inputs at each
size and compare the zero-allocation APIs: this package's `AppendEncode` /
`AppendDecode` against base58-turbo v0.3.0's `encode_into` / `decode_into`.
Representative results on an Intel i7-9700K under WSL2 (AVX2, one pinned
CPU, Go 1.24.0, Rust 1.89.0; Turbo commit `18c8f94`) are:

| Payload | Go encode | Turbo encode | Go decode | Turbo decode |
|---:|---:|---:|---:|---:|
| 16 B | **~44.7 ns** | ~50.4 ns | **~39.2 ns** | ~48.4 ns |
| 32 B | ~43.8 ns | **~38.7 ns** | **~47.7 ns** | ~55.3 ns |
| 48 B | ~136 ns | **~74.0 ns** | ~93.2 ns | **~51.1 ns** |
| 64 B | ~93.2 ns | **~61.8 ns** | ~75.0 ns | **~62.7 ns** |
| 128 B | ~444 ns | **~431 ns** | ~350 ns | **~219 ns** |
| 1000 B | **~12.3 µs** | ~14.1 µs | ~11.7 µs | **~7.30 µs** |

Both implementations allocate zero bytes in these encode-into benchmarks;
this package now also allocates zero bytes for decode through 1 KiB. WSL2
clock/scheduling noise is material, so treat differences below roughly 10%
as parity and reproduce on the deployment CPU before choosing a package.
Turbo's generic SIMD decoder remains clearly ahead at 48–128 bytes and its
large scalar decoder leads at 1 KiB; this Go package leads the measured
16-byte decode and 1 KiB encode cases.

Against this package's previous variable encoder, the retained changes give:

| `AppendEncode` payload | Before | Now | Improvement |
|---:|---:|---:|---:|
| 16 B | ~72 ns | ~37 ns | **~1.9x** |
| 48 B | ~197 ns | ~130 ns | **~1.5x** |
| 65 B | ~318 ns | ~184 ns | **~1.7x** |
| 100 B | ~529 ns | ~375 ns | **~1.4x** |
| 128 B | ~820 ns | ~425 ns | **~1.9x** |
| 1000 B | ~34.9 µs, 2 allocs | ~10.5–12.3 µs, 0 allocs | **~2.8–3.3x** |

The block-radix path is portable; amd64 additionally uses AVX2 for the
18-term block convolution and padded 33–63-byte matrix path. Decoder scratch
size classes remove the prior allocation at 256 B and 1 KiB without adopting
Turbo's 10-character grouping, which regressed the Go 1 KiB decoder from
~10–11 µs to ~14 µs in A/B testing.

#### Large account payloads

Solana account state can reach 10 MiB, so the package has a separate
large-input tier rather than extending the block-Horner algorithm indefinitely.
Encode switches to `math/big`'s recursive base conversion at 32 KiB; decode
switches to a divide-and-conquer tree at 8 KiB. Both produce the same canonical
Bitcoin Base58 representation as the smaller paths.

Single-iteration, single-core measurements from the same machine:

| Binary payload | AppendEncode | Encode scratch | AppendDecode | Decode scratch |
|---:|---:|---:|---:|---:|
| 1 MiB | 1.60 s | 36.0 MB | 0.550 s | 55.7 MB |
| 5 MiB | 20.5 s | 235 MB | 7.39 s | 307 MB |
| 10 MiB | 47.9 s | 485 MB | 26.0 s | 678 MB |

Before the large-input tier, 1 MiB took 11.8 s to encode and 14.1 s to
decode; quadratic scaling made 5/10 MiB impractical. The new path is much
faster, but canonical Base58 conversion of a 10 MiB integer remains expensive.
RPC callers that control the requested encoding should still prefer base64;
the table above is the expected cost when Base58 is required.

For comparison, base58-turbo v0.3.0's allocating unbounded API took 9.74 s
to encode and 5.89 s to decode the same 1 MiB payload on the pinned core. The
new Go tier was approximately 6.1x faster for encode and 10.7x for decode there.
Turbo's 5/10 MiB cases were not run because its unbounded path retains the
quadratic block fold.

For historical context, the earlier generic-API comparison used the same
machine (Go 1.26.1, Intel i7-9700K under WSL2, AVX2, one pinned CPU,
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

The dependency-free corpus benchmarks are checked in as
`BenchmarkBase58_AppendEncodeCorpus` and
`BenchmarkBase58_AppendDecodeCorpus`. Reproduce the Go side with:

```sh
GOMAXPROCS=1 go test -run '^$' \
  -bench 'BenchmarkBase58_Append(Encode|Decode)Corpus' \
  -benchmem -benchtime=1s -count=5
```

Large account benchmarks intentionally run once:

```sh
GOMAXPROCS=1 go test -run '^$' \
  -bench 'BenchmarkBase58_Append(Encode|Decode)Large' \
  -benchmem -benchtime=1x -count=1
```

For the Turbo comparison, mirror `benchmarkCorpus` in a temporary Criterion
benchmark at v0.3.0 and rotate through the same 4,096 values. The module
remains free of comparison-only dependencies.

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
